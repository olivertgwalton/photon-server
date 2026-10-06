package httpapi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type downloads interface {
	AddDownload(ctx context.Context, profile, device, item, part uuid.UUID, q *domain.Quality) (store.Download, error)
	Downloads(ctx context.Context, profile uuid.UUID, device *uuid.UUID) ([]store.Download, error)
	Download(ctx context.Context, profile, id uuid.UUID) (store.Download, error)
}

// conversions remove downloads and serve their conversions' files, from whichever node made them.
type conversions interface {
	Remove(ctx context.Context, profile, id uuid.UUID) error
	File(ctx context.Context, download uuid.UUID) (*os.File, string, error)
}

// downloadScope is whose downloads a list answers.
type downloadScope string

const (
	// scopeDevice is the downloads the device asking asked for.
	scopeDevice downloadScope = "device"
	// scopeProfile is the profile's, on every device.
	scopeProfile downloadScope = "profile"
)

func downloadScopes() []downloadScope { return []downloadScope{scopeDevice, scopeProfile} }

// narrowest is the least max_width a download may ask for: one H.264 macroblock.
const narrowest = 16

type downloadRequestJSON struct {
	TitleID uuid.UUID `json:"title_id"`
	// VersionID is the copy, else the longest on disk.
	VersionID uuid.UUID `json:"version_id,omitzero"`
	// PartID is which of a copy's several files.
	PartID         uuid.UUID `json:"part_id,omitzero"`
	MaxBitrateKbps int       `json:"max_bitrate_kbps"`
	// MaxWidth is the widest the picture may be; zero keeps it as shot.
	MaxWidth int `json:"max_width,omitzero"`
	// VideoCodecs are the codecs the device plays, by FFmpeg's names; a conversion is HEVC where
	// it lists hevc, else H.264, which is all it is taken to play where it lists none.
	VideoCodecs []string `json:"video_codecs,omitzero"`
	// VideoRanges are the ranges it shows, SDR alone where it lists none: HEVC keeps HDR10 or HLG
	// it shows, and anything else is tone mapped to SDR.
	VideoRanges []domain.Range `json:"video_ranges,omitzero"`
}

type downloadJSON struct {
	ID uuid.UUID `json:"id"`
	// DeviceID is the device that asked for it, as the signed-in devices list it.
	DeviceID uuid.UUID `json:"device_id"`
	TitleID  uuid.UUID `json:"title_id"`
	PartID   uuid.UUID `json:"part_id"`
	// Method is direct for the part's own file and transcode for a conversion.
	Method         domain.PlayMethod `json:"method"`
	MaxBitrateKbps int               `json:"max_bitrate_kbps,omitzero"`
	MaxWidth       int               `json:"max_width,omitzero"`
	// VideoCodec and VideoRange are what a conversion's video is encoded to.
	VideoCodec domain.VideoCodec    `json:"video_codec,omitzero"`
	VideoRange domain.Range         `json:"video_range,omitzero"`
	State      domain.DownloadState `json:"state"`
	Progress   float64              `json:"progress"`
	SizeBytes  int64                `json:"size_bytes,omitzero"`
	Error      string               `json:"error,omitzero"`
	// URL is where a ready download is fetched, signed until URLExpiresAt.
	URL          string    `json:"url,omitzero"`
	URLExpiresAt time.Time `json:"url_expires_at,omitzero"`
	CreatedAt    time.Time `json:"created_at"`
}

func (a *API) downloadJSON(d store.Download) downloadJSON {
	out := downloadJSON{
		ID: d.ID, DeviceID: d.Device, TitleID: d.Item, PartID: d.Part, Method: domain.PlayDirect, State: d.State,
		Progress: d.Progress, SizeBytes: d.SizeBytes, Error: d.Error, CreatedAt: d.Created,
	}
	path := "/api/v1/parts/" + d.Part.String() + "/stream"
	if q := d.Quality; q != nil {
		out.Method, out.MaxBitrateKbps, out.MaxWidth = domain.PlayTranscode, q.MaxBitrateKbps, q.MaxWidth
		out.VideoCodec, out.VideoRange = q.Codec, q.Range
		path = "/api/v1/downloads/" + d.ID.String() + "/file"
	}
	if d.State == domain.DownloadReady {
		until := time.Now().Add(streamFor)
		out.URL, out.URLExpiresAt = a.svc.Signer.Sign(path, until), until.UTC().Truncate(time.Second)
	}
	return out
}

// addDownload asks for a film or episode to be downloaded no larger than a video bitrate, and a
// width if given, as Plex's Downloads do: a copy already within both is its own file, ready now;
// another is converted in the background, once for every profile asking for it at that quality.
// A copy of several files is downloaded a file at a time, named by part_id.
func (a *API) addDownload(w http.ResponseWriter, r *http.Request) {
	var req downloadRequestJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.MaxBitrateKbps <= 0 || (req.MaxWidth != 0 && req.MaxWidth < narrowest) {
		writeProblem(w, a.logger, codeInvalidBody, "max_bitrate_kbps is above 0, and max_width 0 or at least "+strconv.Itoa(narrowest))
		return
	}
	profile := sessionOf(r).Profile.ID
	c, err := a.svc.Playing.Playable(r.Context(), profile, req.TitleID, req.VersionID)
	if a.answered(w, r, err) {
		return
	}
	part := c.Parts[0]
	if req.PartID != (uuid.UUID{}) {
		i := slices.IndexFunc(c.Parts, func(p store.PlayPart) bool { return p.ID == req.PartID })
		if i < 0 {
			writeProblem(w, a.logger, codeInvalidBody, "part_id is not one of the copy's files")
			return
		}
		part = c.Parts[i]
	} else if len(c.Parts) > 1 {
		writeProblem(w, a.logger, codeInvalidBody, "part_id names which of the copy's "+strconv.Itoa(len(c.Parts))+" files")
		return
	}
	q := domain.Quality{MaxBitrateKbps: req.MaxBitrateKbps, MaxWidth: req.MaxWidth}
	pc := playback.Copy{Container: c.Container, BitrateKbps: c.BitrateKbps, Streams: c.Streams}
	var convert *domain.Quality
	if !playback.Fits(pc, q) {
		plays := []playback.VideoSupport{{Codec: string(domain.VideoH264)}}
		if len(req.VideoCodecs) > 0 {
			plays = nil
			for _, codec := range req.VideoCodecs {
				plays = append(plays, playback.VideoSupport{Codec: codec, Ranges: req.VideoRanges})
			}
		}
		d, err := playback.Conversion(pc, q, plays, a.svc.Setup.Encoder.HEVC)
		if errors.Is(err, playback.ErrNoCompatibleStream) {
			writeProblem(w, a.logger, codeNoCompatibleStream, "the copy has no video to convert to a codec the device plays")
			return
		}
		q.Codec, q.Range = d.Video.Encode.Codec, d.Video.Encode.Range
		convert = &q
	}
	d, err := a.svc.Downloads.AddDownload(r.Context(), profile, sessionOf(r).ID, req.TitleID, part.ID, convert)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, a.downloadJSON(d))
}

// ownDownloads answers the downloads this device asked for, or the profile's on every device, the
// newest first.
func (a *API) ownDownloads(w http.ResponseWriter, r *http.Request) {
	scope := downloadScope(cmp.Or(r.URL.Query().Get("scope"), string(scopeDevice)))
	session := sessionOf(r)
	var device *uuid.UUID
	switch scope {
	case scopeDevice:
		device = &session.ID
	case scopeProfile:
	default:
		writeProblem(w, a.logger, codeInvalidParameter, fmt.Sprintf("scope is one of %v", downloadScopes()))
		return
	}
	all, err := a.svc.Downloads.Downloads(r.Context(), session.Profile.ID, device)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]downloadJSON, len(all))
	for i, d := range all {
		out[i] = a.downloadJSON(d)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[downloadJSON]{Items: out})
}

// download answers one of the profile's downloads, as far as its conversion has got.
func (a *API) download(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	d, err := a.svc.Downloads.Download(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, a.downloadJSON(d))
}

// removeDownload forgets one of the profile's downloads; a conversion no other download needs is
// stopped and its file removed.
func (a *API) removeDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Conversions.Remove(r.Context(), sessionOf(r).Profile.ID, id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// downloadFile serves a ready conversion in byte ranges, so a download manager resumes it, from
// the node of the cluster that made it.
func (a *API) downloadFile(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	f, address, err := a.svc.Conversions.File(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	if f == nil {
		target, err := url.Parse(address)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		a.proxy(w, r, target)
		return
	}
	a.serveFile(w, r, f, "", http.Header{"Content-Type": {"video/mp4"}})
}
