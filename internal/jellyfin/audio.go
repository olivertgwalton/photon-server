package jellyfin

import (
	"cmp"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// audio serves an audio item's file at the addresses Jellyfin's apps build for audio: universal,
// which an app plays it at without asking for its PlaybackInfo first, and stream. A track is a
// title's (see universal). A theme song is served as it is, as photon's own apps play it, and is
// not counted as media sent, as theirs is not.
func (a *API) audio(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(r.PathValue("file"))
	if name != "universal" && name != "stream" && name != "lyrics" && !strings.HasPrefix(name, "stream.") {
		a.refuse(w, http.StatusNotFound)
		return
	}
	id, ok := a.itemID(w, r)
	if !ok {
		return
	}
	if name == "lyrics" {
		a.lyrics(w, r, id)
		return
	}
	t, err := a.svc.Themes.Theme(r.Context(), id)
	switch {
	case isNotFound(err) && name == "universal":
		a.universal(w, r, id)
		return
	case isNotFound(err):
		a.sending(playback.DeliveryFile, a.stream)(w, r)
		return
	}
	var o blob.Object
	var kind string
	if err == nil {
		o, kind, err = artwork.OpenTheme(r.Context(), a.svc.Pictures, id, t.Source, t.Root, t.Path)
	}
	if isNotFound(err) || errors.Is(err, os.ErrNotExist) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	h := http.Header{"Cache-Control": {"public, max-age=31536000, immutable"}, "X-Content-Type-Options": {"nosniff"}}
	if kind != "" {
		h.Set("Content-Type", kind)
	}
	if err := blob.Serve(w, r, o, "", h); err != nil {
		a.internal(w, r, err)
	}
}

// universal answers Jellyfin's /Audio/{itemId}/universal for a track, which an app plays at, or
// Finamp downloads a transcode from, without asking for its PlaybackInfo, naming what it plays in
// its query: the file as it is where a container and codec it names take it, else the HLS photon
// makes of it, which the app is sent to as Jellyfin's universal sends it.
func (a *API) universal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	c, err := a.svc.Playing.Playable(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, uuid.UUID{})
	if isNotFound(err) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	n, err := a.svc.Network.Network(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	p := universalProfile(r)
	src := mediaSource{SupportsDirectPlay: true}
	req := playbackRequest{DeviceProfile: &p, MaxStreamingBitrate: p.MaxStreamingBitrate}
	if ticks, err := strconv.ParseInt(query(r, "startTimeTicks"), 10, 64); err == nil {
		req.StartTimeTicks = ticks
	}
	a.decide(r, &src, id, uuid.NewV7(), c, req, playback.RemoteLimit(n, a.svc.Reach.Network(r)))
	switch {
	case src.SupportsDirectPlay:
		a.sending(playback.DeliveryFile, a.stream)(w, r)
	case src.TranscodingURL != "":
		http.Redirect(w, r, src.TranscodingURL, http.StatusFound)
	default:
		a.refuse(w, http.StatusUnsupportedMediaType)
	}
}

// universalProfile is the device profile universal's query names, as Jellyfin builds one of it:
// each container it plays, or container|codec, and the codec, container and protocol it takes a
// transcode in, HLS of AAC where it names none.
func universalProfile(r *http.Request) deviceProfile {
	var p deviceProfile
	if kbps, err := strconv.ParseInt(query(r, "maxStreamingBitrate"), 10, 64); err == nil {
		p.MaxStreamingBitrate = kbps
	}
	for _, c := range values(r, "container") {
		container, codec, _ := strings.Cut(c, "|")
		p.DirectPlayProfiles = append(p.DirectPlayProfiles, directPlayProfile{Type: "Audio", Container: container, AudioCodec: codec})
	}
	p.TranscodingProfiles = []transcodingProfile{{
		Type: "Audio", Container: cmp.Or(query(r, "transcodingContainer"), "ts"), AudioCodec: cmp.Or(query(r, "audioCodec"), "aac"),
		Protocol: cmp.Or(query(r, "transcodingProtocol"), "hls"), MaxAudioChannels: query(r, "maxAudioChannels"),
	}}
	return p
}
