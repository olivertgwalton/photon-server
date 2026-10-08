package jellyfin

import (
	"bytes"
	"context"
	"encoding/base64"
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

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type hlsFiles interface {
	Has(playback uuid.UUID) bool
	Resource(ctx context.Context, playback uuid.UUID, name string) (hls.Resource, error)
}

// placer chooses the node that encodes a playback, and opens its remux there.
type placer interface {
	Candidates(ctx context.Context, need playback.Need) ([]domain.Node, error)
	Open(ctx context.Context, node domain.Node, playback uuid.UUID, c store.PlayCopy, o playback.Opening) error
	Self() domain.Node
	Refuse(candidates []domain.Node) error
}

// owners say which node of the cluster runs a playback's HLS.
type owners interface {
	Owner(ctx context.Context, playback uuid.UUID) (string, bool, error)
}

// transcodeFor is how long a TranscodingUrl may be fetched for, as long as any film plays.
const transcodeFor = 48 * time.Hour

// transcode is how a copy is made into HLS for an app, as decided when it asked: carried in its
// TranscodingUrl, signed, so whichever node it is fetched from starts it as decided.
type transcode struct {
	Version  uuid.UUID                `json:"version"`
	Method   domain.PlayMethod        `json:"method"`
	Video    domain.VideoPlan         `json:"video"`
	Audio    *domain.AudioPlan        `json:"audio,omitempty"`
	Subtitle *int                     `json:"subtitle,omitempty"`
	Segments domain.SegmentFormat     `json:"segments"`
	Reasons  []domain.TranscodeReason `json:"reasons,omitempty"`
	StartMS  int64                    `json:"start_ms,omitempty"`
}

func transcodeSubject(item, session uuid.UUID, plan string) string {
	return "jellyfin/" + guid(item) + "/" + guid(session) + "/" + plan
}

// transcodingURL is where an app fetches a copy's HLS: relative, as every app puts its own base
// before it, and carrying the app's token, as no app sends one with media.
func (a *API) transcodingURL(r *http.Request, item, session uuid.UUID, t transcode) string {
	b, _ := json.Marshal(t)
	plan := base64.RawURLEncoding.EncodeToString(b)
	exp, sig := a.svc.Signer.Token(transcodeSubject(item, session, plan), time.Now().Add(transcodeFor))
	q := url.Values{
		"PlaySessionId": {guid(session)}, "MediaSourceId": {guid(t.Version)}, "ApiKey": {appOf(r).Token},
		"Plan": {plan}, "Expires": {exp}, "Signature": {sig},
	}
	return "/videos/" + guid(item) + "/master.m3u8?" + q.Encode()
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
		a.write(w, []byte(carryQuery(res.Text, r.URL.RawQuery, name)))
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
// it answered the request itself, refusing it.
func (a *API) open(w http.ResponseWriter, r *http.Request, item, session uuid.UUID) (done bool) {
	a.opening.Lock()
	defer a.opening.Unlock()
	if a.svc.HLS.Has(session) {
		return false
	}
	if _, elsewhere, err := a.svc.Owners.Owner(r.Context(), session); err != nil {
		a.internal(w, r, err)
		return true
	} else if elsewhere {
		return false
	}
	plan := query(r, "Plan")
	if !a.svc.Signer.Valid(transcodeSubject(item, session, plan), query(r, "Expires"), query(r, "Signature"), time.Now()) {
		w.WriteHeader(http.StatusUnauthorized)
		return true
	}
	b, err := base64.RawURLEncoding.DecodeString(plan)
	var t transcode
	if err == nil {
		err = json.Unmarshal(b, &t)
	}
	if err != nil {
		a.refuse(w, http.StatusBadRequest)
		return true
	}
	s := sessionOf(r)
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
	// the next where it has none by then; one copied, here.
	candidates := []domain.Node{a.svc.Placer.Self()}
	if t.Video.Encode != nil {
		if candidates, err = a.svc.Placer.Candidates(r.Context(), playback.NeedOf(t.Video)); err != nil {
			a.internal(w, r, err)
			return true
		}
	}
	o := playback.Opening{
		Profile: s.Profile.ID, Item: item, Version: c.Version, Video: t.Video, Audio: t.Audio, Segments: t.Segments, StartMS: t.StartMS,
	}
	for _, node := range candidates {
		card := playback.Card(s, a.svc.Proxies.Client(r).String(), title, c, d, domain.ChosenTracks{Subtitle: t.Subtitle})
		card.Acceleration = hls.EncodedOn(node.Encoder.Acceleration, t.Video)
		p, err := a.svc.Playbacks.Start(r.Context(), session, t.Method, card, node.ID)
		if errors.Is(err, playback.ErrStarted) {
			// Another node, asked at once, runs it: the request is handed on to it.
			return false
		} else if err != nil {
			a.internal(w, r, err)
			return true
		}
		err = a.svc.Placer.Open(r.Context(), node, session, c, o)
		if err == nil {
			a.svc.Playbacks.Opened(r.Context(), p)
			// Here, the request is answered; elsewhere, handed on to the node that runs it.
			return false
		}
		if aerr := a.svc.Playbacks.Abandon(context.WithoutCancel(r.Context()), session); aerr != nil {
			a.internal(w, r, aerr)
			return true
		}
		if !errors.Is(err, hls.ErrTranscodeLimit) {
			a.internal(w, r, err)
			return true
		}
	}
	a.logger.InfoContext(r.Context(), "jellyfin transcode refused", slog.Any("err", a.svc.Placer.Refuse(candidates)))
	w.Header().Set("Retry-After", "30")
	a.refuse(w, http.StatusServiceUnavailable)
	return true
}

// fromOwner hands a request for a play session another node runs to that node, by the signed
// address its own API serves the session's HLS at, carrying the app's query into the playlists it
// answers.
func (a *API) fromOwner(w http.ResponseWriter, r *http.Request, session uuid.UUID, name string) {
	address, elsewhere, err := a.svc.Owners.Owner(r.Context(), session)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	target, perr := url.Parse(address)
	if !elsewhere || perr != nil {
		a.refuse(w, http.StatusNotFound)
		return
	}
	subject := "/api/v1/hls/" + session.String()
	exp, sig := a.svc.Signer.Token(subject, time.Now().Add(time.Hour))
	appQuery := r.URL.RawQuery
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

// carryQuery puts the app's query on every address a playlist names, its own lines and URI
// attributes, as no app sends its token with what it fetches of HLS.
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
	_, err := a.svc.Playbacks.Finish(r.Context(), sessionOf(r).Profile.ID, id)
	if err != nil && !errors.Is(err, playback.ErrNoPlayback) {
		a.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
