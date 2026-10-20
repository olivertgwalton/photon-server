package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type hlsFiles interface {
	Has(playback uuid.UUID) bool
	Resource(ctx context.Context, playback uuid.UUID, name string) (hls.Resource, error)
	Extracted(ctx context.Context, src hls.SubtitleSource, want string) (string, error)
	WebVTT(ctx context.Context, open func() (media.Input, error), language string) (string, error)
	Cues(ctx context.Context, open func() (media.Input, error), language string) ([]hls.Cue, error)
	VobSub(ctx context.Context, open func() (media.Input, error)) ([]byte, error)
}

// placer chooses the node that delivers a playback, encoding it where it is encoded, and opens its
// remux there.
type placer interface {
	Candidates(ctx context.Context, need playback.Need) ([]domain.Node, error)
	Deliverers(ctx context.Context, network domain.ClientNetwork) ([]domain.Node, error)
	Open(ctx context.Context, node domain.Node, playback uuid.UUID, c store.PlayCopy, o playback.Opening) error
	Self() domain.Node
	Refuse(candidates []domain.Node, refused error) error
}

// owners say which node of the cluster runs a playback's HLS.
type owners interface {
	Owner(ctx context.Context, playback uuid.UUID) (domain.Node, bool, error)
}

// plans keep how each play session's HLS is to be made, for whichever node is asked for it.
type plans interface {
	SavePlan(ctx context.Context, session uuid.UUID, plan []byte, ttl time.Duration) error
	Plan(ctx context.Context, session uuid.UUID) ([]byte, bool, error)
}

// transcodeFor is how long a TranscodingUrl may be fetched for, as long as any film plays.
const transcodeFor = 48 * time.Hour

// planned is a play session's plan as kept: whose it is, of what, and the transcode.
type planned struct {
	Profile   uuid.UUID `json:"profile"`
	Item      uuid.UUID `json:"item"`
	Transcode transcode `json:"transcode"`
}

// transcode is how a copy is made into HLS for an app, as decided when it asked: kept under its
// play session (see plans), so whichever node its TranscodingUrl is fetched from starts it as
// decided.
type transcode struct {
	Version  uuid.UUID                `json:"version"`
	Method   domain.PlayMethod        `json:"method"`
	Video    domain.VideoPlan         `json:"video"`
	Audio    *domain.AudioPlan        `json:"audio,omitempty"`
	Subtitle *int                     `json:"subtitle,omitempty"`
	Segments domain.SegmentFormat     `json:"segments"`
	Reasons  []domain.TranscodeReason `json:"reasons,omitempty"`
	StartMS  int64                    `json:"start_ms,omitempty"`
	// Renditions leave out of the HLS the subtitle files the app fetches itself.
	Renditions playback.Renditions `json:"renditions"`
}

// transcodingURL keeps a play session's plan and answers where an app fetches its HLS: relative, as
// every app puts its own base before it, and carrying a key for the app's token, as no app sends
// one with media.
func (a *API) transcodingURL(r *http.Request, item, session uuid.UUID, t transcode, copyVideo, copyAudio *bool) (string, error) {
	b, err := json.Marshal(planned{Profile: auth.SessionOf(r.Context()).Profile.ID, Item: item, Transcode: t})
	if err != nil {
		return "", err
	}
	if err := a.svc.Plans.SavePlan(r.Context(), session, b, transcodeFor); err != nil {
		return "", err
	}
	q := url.Values{"PlaySessionId": {guid(session)}, "MediaSourceId": {guid(t.Version)}, "ApiKey": {a.mediaKey(r)}}
	// Jellyfin's web app falls back again on an error until its stream's address refuses both
	// copies, which Jellyfin's own address never says.
	for name, allowed := range map[string]*bool{"allowVideoStreamCopy": copyVideo, "allowAudioStreamCopy": copyAudio} {
		if allowed != nil && !*allowed {
			q.Set(name, "false")
		}
	}
	return "/videos/" + guid(item) + "/master.m3u8?" + q.Encode(), nil
}

// video serves what is under a video's own path: its file as it is (stream, stream.mkv), or its
// HLS by the names its playlists give.
func (a *API) video(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if lower := strings.ToLower(name); lower == "stream" || strings.HasPrefix(lower, "stream.") {
		a.sending(playback.DeliveryFile, a.stream)(w, r)
		return
	}
	a.hls(w, r, name)
}

// hls serves a play session's HLS: its master playlist starts it where no node runs it yet, and the
// rest is served by the node that runs it.
func (a *API) hls(w http.ResponseWriter, r *http.Request, name string) {
	item, ok := a.itemID(w, r)
	if !ok {
		return
	}
	session, err := uuid.Parse(query(r, "PlaySessionId"))
	if err != nil {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if name == "master.m3u8" || name == "main.m3u8" {
		name = hls.MasterName
		if !a.svc.HLS.Has(session) {
			if done := a.open(w, r, item, session); done {
				return
			}
		}
	}
	if !a.svc.HLS.Has(session) {
		a.fromOwner(w, r, session, name)
		return
	}
	a.sending(playback.DeliverySegment, func(w http.ResponseWriter, r *http.Request) { a.serveHLS(w, r, session, name) })(w, r)
}

// serveHLS serves a file of a play session this node runs.
func (a *API) serveHLS(w http.ResponseWriter, r *http.Request, session uuid.UUID, name string) {
	res, err := a.svc.HLS.Resource(r.Context(), session, name)
	if errors.Is(err, hls.ErrNoRemux) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", res.Type)
	if res.File == nil {
		a.write(w, []byte(carryQuery(res.Text, a.playlistQuery(r), name)))
		return
	}
	defer res.File.Close()
	info, err := res.File.Stat()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), res.File)
}

// open starts a play session's HLS as its TranscodingUrl says, once, on this node; done is whether
// it answered the request itself, refusing it, or its app went while another fetch started it.
func (a *API) open(w http.ResponseWriter, r *http.Request, item, session uuid.UUID) (done bool) {
	started, ok := a.starting(r.Context(), session)
	if !ok {
		return true
	}
	defer started()
	if a.svc.HLS.Has(session) {
		return false
	}
	if _, elsewhere, err := a.svc.Owners.Owner(r.Context(), session); err != nil {
		a.internal(w, r, err)
		return true
	} else if elsewhere {
		return false
	}
	s := auth.SessionOf(r.Context())
	b, ok, err := a.svc.Plans.Plan(r.Context(), session)
	var p planned
	if err == nil && ok {
		err = json.Unmarshal(b, &p)
	}
	if err != nil {
		a.internal(w, r, err)
		return true
	}
	// A session another profile asked for, or of another title, is one this app has not.
	if !ok || p.Profile != s.Profile.ID || p.Item != item {
		a.refuse(w, http.StatusNotFound)
		return true
	}
	t := p.Transcode
	c, err := a.svc.Playing.Playable(r.Context(), s.Profile.ID, item, t.Version)
	if isNotFound(err) {
		a.refuse(w, http.StatusNotFound)
		return true
	}
	if err != nil {
		a.internal(w, r, err)
		return true
	}
	title, err := a.svc.Playing.PlaybackTitle(r.Context(), item)
	if err != nil {
		a.internal(w, r, err)
		return true
	}
	d := playback.Decision{Method: t.Method, Video: &t.Video, Audio: t.Audio, Reasons: t.Reasons}
	// A copy encoded is opened on the node with the most transcode slots free that can encode it,
	// the next where it has none by then; one copied, on the node best placed to deliver it.
	var candidates, deliverers []domain.Node
	if t.Video.Encode != nil {
		candidates, err = a.svc.Placer.Candidates(r.Context(), playback.NeedOf(t.Video))
	} else {
		deliverers, err = a.svc.Placer.Deliverers(r.Context(), a.svc.Reach.Network(r))
	}
	if err != nil {
		a.internal(w, r, err)
		return true
	}
	_, _, err = playback.Place(r.Context(), a.svc.Placer, a.svc.Playbacks, candidates, deliverers, playback.Start{
		ID: session, Session: s, Address: a.svc.Reach.Client(r).String(), Title: title, Copy: c, Tracks: domain.ChosenTracks{Subtitle: t.Subtitle},
		Opening: playback.Opening{Profile: s.Profile.ID, Item: item, Version: c.Version, Segments: t.Segments, StartMS: t.StartMS, Renditions: t.Renditions},
	}, func(playback.Encoding) (playback.Decision, error) { return d, nil })
	switch {
	case err == nil, errors.Is(err, playback.ErrStarted):
		// Here, the request is answered; elsewhere, by another node asked at once, it is handed on
		// to the node that runs it.
		return false
	case errors.Is(err, hls.ErrTranscodeLimit):
		a.logger.InfoContext(r.Context(), "jellyfin transcode refused", slog.Any("err", a.svc.Placer.Refuse(candidates, err)))
		w.Header().Set("Retry-After", "30")
		a.refuse(w, http.StatusServiceUnavailable)
		return true
	case errors.Is(err, playback.ErrStreamLimit):
		a.logger.InfoContext(r.Context(), "jellyfin stream refused", slog.Any("err", err))
		w.Header().Set("Retry-After", "30")
		a.refuse(w, http.StatusServiceUnavailable)
		return true
	}
	a.internal(w, r, err)
	return true
}

// starting waits while another request starts session, then marks it started by this one until
// started is called; ok is false if ctx ends first.
func (a *API) starting(ctx context.Context, session uuid.UUID) (started func(), ok bool) {
	for {
		a.openingMu.Lock()
		other, busy := a.opening[session]
		if !busy {
			a.opening[session] = make(chan struct{})
			a.openingMu.Unlock()
			return func() {
				a.openingMu.Lock()
				close(a.opening[session])
				delete(a.opening, session)
				a.openingMu.Unlock()
			}, true
		}
		a.openingMu.Unlock()
		select {
		case <-other:
		case <-ctx.Done():
			return nil, false
		}
	}
}

// fromOwner sends a request for a play session another node runs to the signed address that node's
// own API serves the session's HLS at: at the node itself, where the app reaches it, so the rest
// is fetched from there too; else through this node, carrying the app's query into the playlists
// it answers.
func (a *API) fromOwner(w http.ResponseWriter, r *http.Request, session uuid.UUID, name string) {
	owner, elsewhere, err := a.svc.Owners.Owner(r.Context(), session)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	target, perr := url.Parse(owner.Address)
	if !elsewhere || perr != nil {
		a.refuse(w, http.StatusNotFound)
		return
	}
	subject := "/api/v1/hls/" + session.String()
	if base := owner.URLFor(a.svc.Reach.Network(r)); base != "" {
		exp, sig := a.svc.Signer.Token(subject, time.Now().Add(transcodeFor))
		http.Redirect(w, r, base+subject+"/"+exp+"/"+sig+"/"+name, http.StatusFound)
		return
	}
	exp, sig := a.svc.Signer.Token(subject, time.Now().Add(time.Hour))
	appQuery := a.playlistQuery(r)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = subject + "/" + exp + "/" + sig + "/" + name
			pr.Out.URL.RawPath, pr.Out.URL.RawQuery = "", ""
			pr.Out.Header.Del("Authorization")
			pr.SetXForwarded()
		},
		ModifyResponse: func(resp *http.Response) error {
			if !strings.HasSuffix(name, ".m3u8") || resp.StatusCode != http.StatusOK {
				return nil
			}
			b, err := io.ReadAll(resp.Body)
			if err := errors.Join(err, resp.Body.Close()); err != nil {
				return err
			}
			b = []byte(carryQuery(string(b), appQuery, name))
			resp.Body, resp.ContentLength = io.NopCloser(bytes.NewReader(b)), int64(len(b))
			resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
			return nil
		},
	}
	proxy.ServeHTTP(w, r)
}

// playlistQuery is what an address a playlist names carries of the app's query: a key in place of
// its token, as no app sends one with what it fetches of HLS, and its play session. The plan, its
// signature and the copy only open the session, at the master; carried on every line, they made a
// playlist about ten times its size.
func (a *API) playlistQuery(r *http.Request) string {
	q := url.Values{"PlaySessionId": {query(r, "PlaySessionId")}}
	if query(r, "ApiKey") != "" {
		q.Set("ApiKey", a.mediaKey(r))
	}
	return q.Encode()
}

// carryQuery puts query on every address a playlist names, its own lines and URI attributes.
func carryQuery(playlist, query, name string) string {
	if query == "" || !strings.HasSuffix(name, ".m3u8") {
		return playlist
	}
	with := func(uri string) string {
		if strings.Contains(uri, "?") {
			return uri + "&" + query
		}
		return uri + "?" + query
	}
	lines := strings.Split(playlist, "\n")
	for n, line := range lines {
		switch {
		case line == "":
		case !strings.HasPrefix(line, "#"):
			lines[n] = with(line)
		case strings.Contains(line, `URI="`):
			before, rest, _ := strings.Cut(line, `URI="`)
			uri, after, _ := strings.Cut(rest, `"`)
			lines[n] = before + `URI="` + with(uri) + `"` + after
		}
	}
	return strings.Join(lines, "\n")
}

// endEncoding ends a play session of the profile's, where its HLS was, as the app moves to another
// stream of the title.
func (a *API) endEncoding(w http.ResponseWriter, r *http.Request) {
	id := playID(query(r, "playSessionId"))
	_, err := a.svc.Playbacks.Finish(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if err != nil && !errors.Is(err, playback.ErrNoPlayback) {
		a.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
