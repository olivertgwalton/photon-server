//go:build integration

package jellyfin

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
	"github.com/olivertgwalton/photon-server/internal/testtool"
)

const jellyfinWeb = `MediaBrowser Client="Jellyfin Web", Device="Firefox", DeviceId="TW96", Version="10.11.0", Token="pst_ada"`

// pgsStream and assStream are the subtitle streams of the film aSubtitledFilm makes, and assFile,
// vobSubFile, pgsFile and srtFile the indices of the files beside it, after them, by their names.
const (
	pgsStream, assStream                  = 2, 3
	assFile, vobSubFile, pgsFile, srtFile = 4, 5, 6, 7
)

// aSubtitledFilm is a film in an MKV of H.264 and AAC carrying PGS and ASS, with ASS, VobSub, PGS
// and SubRip files beside it, the SubRip French in Windows-1252, served by an API that reads its
// subtitles with FFmpeg, and its copy.
func aSubtitledFilm(t *testing.T) (api http.Handler, film, copyID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	tool := func(name, env string) string {
		path, err := exec.LookPath(cmp.Or(os.Getenv(env), name))
		if err != nil {
			t.Skipf("needs %s: %v", name, err)
		}
		return path
	}
	ffmpeg, ffprobe := tool("ffmpeg", "PHOTON_FFMPEG"), tool("ffprobe", "PHOTON_FFPROBE")
	root, work := t.TempDir(), t.TempDir()
	ass := "[Script Info]\nScriptType: v4.00+\nPlayResX: 320\nPlayResY: 240\n\n[V4+ Styles]\n" +
		"Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
		"Style: Default,Arial,24,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,2,10,10,10,1\n\n[Events]\n" +
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,Hello\n"
	for name, body := range map[string][]byte{"en.sup": testtool.PGS(0, 2*90000), "en.ass": []byte(ass)} {
		if err := os.WriteFile(filepath.Join(work, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "Akira"), 0o755); err != nil {
		t.Fatal(err)
	}
	made := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=24", "-f", "lavfi", "-i", "sine",
		"-i", filepath.Join(work, "en.sup"), "-i", filepath.Join(work, "en.ass"),
		"-t", "4", "-map", "0", "-map", "1", "-map", "2", "-map", "3", "-c:v", "libx264", "-c:a", "aac", "-c:s", "copy",
		filepath.Join(root, "Akira", "Akira.mkv"))
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	// FFmpeg writes a VobSub's .sub, MPEG program stream, but no .idx.
	made = exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-fix_sub_duration",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=24", "-i", filepath.Join(work, "en.sup"),
		"-t", "4", "-map", "1", "-c:s", "dvdsub", "-f", "vob", filepath.Join(root, "Akira", "Akira.en.sub"))
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the VobSub: %v: %s", err, out)
	}
	idx := "# VobSub index file, v7 (do not modify this line!)\nsize: 320x240\npalette: 000000, ffffff, 000000, 000000, 000000, 000000, 000000, 000000, " +
		"000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000\n\nid: en, index: 0\ntimestamp: 00:00:00:000, filepos: 000000000\n"
	for name, body := range map[string][]byte{
		"Akira.en.ass": []byte(ass), "Akira.en.idx": []byte(idx), "Akira.en.sup": testtool.PGS(0, 2*90000),
		"Akira.fr.srt": []byte("1\r\n00:00:01,000 --> 00:00:03,000\r\n<i>Caf\xe9</i>\r\n\r\n"),
	} {
		if err := os.WriteFile(filepath.Join(root, "Akira", name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sidecar := func(name, codec string, lang language.Tag) store.Subtitle {
		return store.Subtitle{RelPath: "Akira/" + name, Size: 1, ModTime: time.Unix(0, 0), Codec: codec, Language: lang}
	}

	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, db, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, root)
	if err != nil {
		t.Fatal(err)
	}
	part := store.Part{RelPath: "Akira/Akira.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
		Size: 1, Duration: 4 * time.Second, Container: "matroska,webm",
		Streams: []domain.Stream{
			{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 320, Height: 240, Range: domain.RangeSDR},
			{Index: 1, Kind: domain.StreamAudio, Codec: "aac", Channels: 1},
			{Index: pgsStream, Kind: domain.StreamSubtitle, Codec: "hdmv_pgs_subtitle", Language: language.English},
			{Index: assStream, Kind: domain.StreamSubtitle, Codec: "ass", Language: language.English},
		},
	}}
	if _, err := st.SaveFolder(ctx, films.ID, "Akira", []byte("v"), []store.Film{{Title: "Akira", Folder: "Akira", Copies: []store.Copy{
		{ContentKey: []byte("akira"), Parts: []store.Part{part}, Subtitles: []store.Subtitle{
			sidecar("Akira.en.ass", "ass", language.English), sidecar("Akira.en.idx", "dvd_subtitle", language.English),
			sidecar("Akira.en.sup", "hdmv_pgs_subtitle", language.English), sidecar("Akira.fr.srt", "subrip", language.French),
		}},
	}}}, nil); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, []uuid.UUID{films.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	film = cards[0].ID
	versions, err := st.Versions(ctx, []uuid.UUID{film})
	if err != nil {
		t.Fatal(err)
	}
	remuxer, err := hls.NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: ffmpeg}, FFprobe: media.Tool{Path: ffprobe}}, t.TempDir(), t.TempDir(),
		hls.Hardware{Accel: domain.AccelSoftware}, hls.Unlimited, log)
	if err != nil {
		t.Fatal(err)
	}
	api = New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{
		Copies: noCopies{}, Discover: noDiscoveries{}, Sent: playback.NewSent(), Network: st,
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Parts: library.Parts{Places: st}, Playbacks: newFakePlaybacks(),
		Watching: st, Preferences: st, HLS: remuxer, Placer: alone(newFakeRemuxes()), Owners: noOwners{}, Signer: playback.NewSigner([]byte("key")),
	})
	return api, film, versions[film][0].ID
}

// An app asks for a subtitle stream of a file as Jellyfin's apps do, at the address Jellyfin
// gives it: the stream is read out of the file as it is, in the format its name asks for, from
// whatever start the address gives, and one it is not read out in is refused.
func TestAnAppIsSentASubtitleStreamReadOutOfTheFile(t *testing.T) {
	api, film, copyID := aSubtitledFilm(t)
	get := func(index, file string, want int) []byte {
		t.Helper()
		w := serve(api, http.MethodGet, "/Videos/"+guid(film)+"/"+guid(copyID)+"/Subtitles/"+index+"/"+file, jellyfinWeb, "")
		if w.Code != want {
			t.Fatalf("%s/%s: %d %s, want %d", index, file, w.Code, w.Body, want)
		}
		return w.Body.Bytes()
	}
	for _, file := range []string{"0/Stream.pgssub", "Stream.sup", "600000000/Stream.pgssub"} {
		if got := get("2", file, http.StatusOK); !bytes.Equal(got, testtool.PGS(0, 2*90000)) {
			t.Errorf("PGS at %s = %x, want the stream as it is", file, got)
		}
	}
	for _, file := range []string{"0/Stream.ass", "0/Stream.ssa"} {
		if got := get("3", file, http.StatusOK); !strings.Contains(string(got), "Hello") {
			t.Errorf("ASS at %s = %q, want the stream as it is", file, got)
		}
	}
	get("2", "0/Stream.vtt", http.StatusNotFound)
	get("2", "0/Stream.js", http.StatusNotFound)
	get("1", "0/Stream.sup", http.StatusNotFound)
}

// An app asks for a subtitle file beside a copy as Jellyfin's apps do, by its index, in the format
// its name asks for: plain text as WebVTT, read in its charset, or as the JSON of its cues Jellyfin's
// web app reads; VobSub in Matroska; any file as it is in its own format; and none in a format photon
// does not make.
func TestAnAppIsSentASubtitleFileInTheFormatItAsks(t *testing.T) {
	api, film, copyID := aSubtitledFilm(t)
	get := func(index int, file string, want int) []byte {
		t.Helper()
		w := serve(api, http.MethodGet, "/Videos/"+guid(film)+"/"+guid(copyID)+"/Subtitles/"+strconv.Itoa(index)+"/0/"+file, jellyfinWeb, "")
		if w.Code != want {
			t.Fatalf("%d/%s: %d %s, want %d", index, file, w.Code, w.Body, want)
		}
		return w.Body.Bytes()
	}
	if got := string(get(srtFile, "Stream.vtt", http.StatusOK)); !strings.HasPrefix(got, "WEBVTT") || !strings.Contains(got, "00:01.000 --> 00:03.000\n<i>Café</i>") {
		t.Errorf("the SubRip as WebVTT = %q", got)
	}
	var track struct{ TrackEvents []trackEvent }
	if err := json.Unmarshal(get(srtFile, "Stream.js", http.StatusOK), &track); err != nil ||
		len(track.TrackEvents) != 1 || track.TrackEvents[0] != (trackEvent{ID: "1", Text: "<i>Café</i>", StartPositionTicks: 10_000_000, EndPositionTicks: 30_000_000}) {
		t.Errorf("the SubRip as JSON = %+v, %v", track, err)
	}
	if got := get(srtFile, "Stream.srt", http.StatusOK); !bytes.Contains(got, []byte("Caf\xe9")) {
		t.Errorf("the SubRip as it is = %q", got)
	}
	if got := get(assFile, "Stream.ssa", http.StatusOK); !strings.Contains(string(got), "Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,Hello") {
		t.Errorf("the ASS as it is = %q", got)
	}
	if got := get(pgsFile, "Stream.pgssub", http.StatusOK); !bytes.Equal(got, testtool.PGS(0, 2*90000)) {
		t.Errorf("the PGS as it is = %x", got)
	}
	if got := get(vobSubFile, "Stream.vobsub", http.StatusOK); !bytes.HasPrefix(got, []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		t.Errorf("the VobSub = %x, want it in Matroska", got[:min(len(got), 16)])
	}
	for _, refused := range []struct {
		index int
		file  string
	}{{srtFile, "Stream.ass"}, {assFile, "Stream.vtt"}, {assFile, "Stream.js"}, {pgsFile, "Stream.vtt"}, {vobSubFile, "Stream.idx"}, {srtFile + 1, "Stream.srt"}} {
		get(refused.index, refused.file, http.StatusNotFound)
	}
}

// playbackInfo is what PlaybackInfo answers a Jellyfin web app of a profile for a film, with a
// subtitle chosen.
func playbackInfo(t *testing.T, api http.Handler, film uuid.UUID, subtitle int, profile string) map[string]any {
	t.Helper()
	w := serve(api, http.MethodPost, "/Items/"+guid(film)+"/PlaybackInfo", jellyfinWeb,
		`{"SubtitleStreamIndex":`+strconv.Itoa(subtitle)+`,"DeviceProfile":`+profile+`}`)
	var info struct{ MediaSources []map[string]any }
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || w.Code != http.StatusOK || len(info.MediaSources) != 1 {
		t.Fatalf("PlaybackInfo: %d %s", w.Code, w.Body)
	}
	return info.MediaSources[0]
}

// subtitleStream is a source's subtitle stream at an index.
func subtitleStream(t *testing.T, src map[string]any, index int) map[string]any {
	t.Helper()
	streams, _ := src["MediaStreams"].([]any)
	for _, s := range streams {
		if m, _ := s.(map[string]any); m["Type"] == "Subtitle" && m["Index"] == float64(index) {
			return m
		}
	}
	t.Fatalf("no subtitle stream %d in %v", index, streams)
	return nil
}

// A picture subtitle is named as Jellyfin names it, by which Jellyfin's web app draws PGS itself.
func TestAPictureSubtitleIsNamedAsJellyfinNamesIt(t *testing.T) {
	api, film, _ := aSubtitledFilm(t)
	src := playbackInfo(t, api, film, pgsStream, `{}`)
	if pgs, ass := subtitleStream(t, src, pgsStream), subtitleStream(t, src, assStream); pgs["Codec"] != "PGSSUB" || ass["Codec"] != "ass" {
		t.Errorf("codecs %v and %v, want PGSSUB and ass", pgs["Codec"], ass["Codec"])
	}
}

// webProfile is Jellyfin's web app in a browser that plays MKV of H.264 and AAC, as its profile
// says, and draws ASS, SSA and PGS itself beside the video, with JASSUB and libbitsub.
const webProfile = `{
	"DirectPlayProfiles": [{"Type": "Video", "Container": "mkv,webm", "VideoCodec": "h264,vp9", "AudioCodec": "aac,opus"}],
	"TranscodingProfiles": [{"Type": "Video", "Container": "mp4", "Protocol": "hls", "VideoCodec": "h264", "AudioCodec": "aac", "MaxAudioChannels": "2"}],
	"SubtitleProfiles": [{"Format": "vtt", "Method": "External"}, {"Format": "ass", "Method": "External"},
		{"Format": "ssa", "Method": "External"}, {"Format": "pgssub", "Method": "External"},
		{"Format": "vobsub", "Container": "mks", "Method": "External"}]
}`

// An app that draws PGS itself beside the video plays a file carrying it as it is, the stream
// read out for it at the address PlaybackInfo gives it, as Jellyfin's DeliveryUrl; an app that
// does not is sent HLS with the subtitle drawn in.
func TestAnAppThatDrawsPGSItselfPlaysTheFileAsItIs(t *testing.T) {
	api, film, copyID := aSubtitledFilm(t)
	src := playbackInfo(t, api, film, pgsStream, webProfile)
	if src["SupportsDirectPlay"] != true || src["TranscodingUrl"] != nil {
		t.Fatalf("the web app's source: %v, want the file as it is", src)
	}
	base := "/Videos/" + guid(film) + "/" + guid(copyID) + "/Subtitles/"
	pgs, ass := subtitleStream(t, src, pgsStream), subtitleStream(t, src, assStream)
	if pgs["DeliveryMethod"] != "External" || pgs["DeliveryUrl"] != base+"2/0/Stream.pgssub?ApiKey=pst_ada" {
		t.Errorf("the PGS stream: %v", pgs)
	}
	if ass["DeliveryMethod"] != "External" || ass["DeliveryUrl"] != base+"3/0/Stream.ass?ApiKey=pst_ada" {
		t.Errorf("the ASS stream: %v", ass)
	}
	// The app fetches it with no header, as libbitsub does.
	if w := serve(api, http.MethodGet, pgs["DeliveryUrl"].(string), "", ""); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), testtool.PGS(0, 2*90000)) {
		t.Errorf("fetching the PGS stream: %d %x", w.Code, w.Body)
	}

	src = playbackInfo(t, api, film, pgsStream, strings.Replace(webProfile, `{"Format": "pgssub", "Method": "External"}`, `{"Format": "pgssub", "Method": "Encode"}`, 1))
	transcoding, _ := src["TranscodingUrl"].(string)
	u, err := url.Parse(transcoding)
	if err != nil || src["SupportsDirectPlay"] != false {
		t.Fatalf("a source for an app that draws no PGS: %v", src)
	}
	plan, err := base64.RawURLEncoding.DecodeString(u.Query().Get("Plan"))
	var tc transcode
	if err != nil || json.Unmarshal(plan, &tc) != nil || tc.Video.Encode == nil || tc.Video.Encode.Burn == nil || *tc.Video.Encode.Burn != pgsStream {
		t.Errorf("HLS for an app that draws no PGS: %s, want the PGS stream drawn in", plan)
	}
	if pgs := subtitleStream(t, src, pgsStream); pgs["DeliveryMethod"] != "Encode" || pgs["DeliveryUrl"] != nil {
		t.Errorf("the PGS stream drawn in: %v", pgs)
	}
}

// Jellyfin's web app plays a film as it is with a subtitle file beside it chosen, drawing the file
// itself from the address PlaybackInfo gives it, as Jellyfin's DeliveryUrl, in a format it draws:
// plain text as WebVTT, whose JSON it reads at the same address by the name Stream.js.
func TestJellyfinWebDrawsASubtitleFileItself(t *testing.T) {
	api, film, copyID := aSubtitledFilm(t)
	src := playbackInfo(t, api, film, srtFile, webProfile)
	if src["SupportsDirectPlay"] != true || src["TranscodingUrl"] != nil {
		t.Fatalf("the web app's source with the SubRip chosen: %v, want the file as it is", src)
	}
	base := "/Videos/" + guid(film) + "/" + guid(copyID) + "/Subtitles/"
	for index, want := range map[int]string{srtFile: "7/0/Stream.vtt", assFile: "4/0/Stream.ass", pgsFile: "6/0/Stream.pgssub", vobSubFile: "5/0/Stream.vobsub"} {
		if s := subtitleStream(t, src, index); s["DeliveryMethod"] != "External" || s["DeliveryUrl"] != base+want+"?ApiKey=pst_ada" {
			t.Errorf("the file at %d: %v, want it at %s", index, s, want)
		}
	}
	srt := subtitleStream(t, src, srtFile)["DeliveryUrl"].(string)
	var track struct{ TrackEvents []trackEvent }
	if w := serve(api, http.MethodGet, strings.Replace(srt, ".vtt", ".js", 1), "", ""); w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &track) != nil ||
		len(track.TrackEvents) != 1 || track.TrackEvents[0].Text != "<i>Café</i>" {
		t.Errorf("the SubRip's cues: %d %s", w.Code, w.Body)
	}
}

// Jellyfin's web app given HLS draws a subtitle file beside the copy itself, as it does the file
// played as it is, fetched whole from the address PlaybackInfo gives it: the HLS keeps the film's
// clock, so a cue is shown at its own time from wherever the app starts or seeks, and none is
// offered in the HLS too. One drawn into the picture is said to be, and so is a file the app draws
// in no format, which it is never sent to fetch.
func TestJellyfinWebDrawsASubtitleFileBesideHLS(t *testing.T) {
	api, film, copyID := aSubtitledFilm(t)
	burns := strings.Replace(webProfile, `{"Format": "pgssub", "Method": "External"}`, `{"Format": "pgssub", "Method": "Encode"}`, 1)
	src := playbackInfo(t, api, film, pgsStream, burns)
	transcoding, _ := src["TranscodingUrl"].(string)
	u, err := url.Parse(transcoding)
	if err != nil || transcoding == "" {
		t.Fatalf("the web app's source with the PGS stream drawn in: %v, want HLS", src)
	}
	plan, err := base64.RawURLEncoding.DecodeString(u.Query().Get("Plan"))
	var tc transcode
	if err != nil || json.Unmarshal(plan, &tc) != nil || tc.Renditions != playback.RenditionsEmbedded {
		t.Errorf("HLS for the web app: %s, want no subtitle file in it", plan)
	}
	base := "/Videos/" + guid(film) + "/" + guid(copyID) + "/Subtitles/"
	for index, want := range map[int]string{srtFile: "7/0/Stream.vtt", assFile: "4/0/Stream.ass", assStream: "3/0/Stream.ass", vobSubFile: "5/0/Stream.vobsub"} {
		if s := subtitleStream(t, src, index); s["DeliveryMethod"] != "External" || s["DeliveryUrl"] != base+want+"?ApiKey=pst_ada" {
			t.Errorf("the subtitle at %d: %v, want it at %s", index, s, want)
		}
	}
	for _, index := range []int{pgsStream, pgsFile} {
		if s := subtitleStream(t, src, index); s["DeliveryMethod"] != "Encode" || s["DeliveryUrl"] != nil {
			t.Errorf("the subtitle at %d: %v, want it drawn in", index, s)
		}
	}
	srt := subtitleStream(t, src, srtFile)["DeliveryUrl"].(string)
	if w := serve(api, http.MethodGet, srt, "", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "00:01.000 --> 00:03.000") {
		t.Errorf("the SubRip as WebVTT: %d %s", w.Code, w.Body)
	}
}

// Jellyfin's web app starts its player on the subtitle PlaybackInfo's source names its default, so
// the source names the one the app chose, a stream of the file or a file beside it, played as it is
// or as HLS, and none where it chose none, as Jellyfin answers it.
func TestAnAppStartsOnTheSubtitleItChose(t *testing.T) {
	api, film, _ := aSubtitledFilm(t)
	burns := strings.Replace(webProfile, `{"Format": "pgssub", "Method": "External"}`, `{"Format": "pgssub", "Method": "Encode"}`, 1)
	for _, c := range []struct {
		name     string
		subtitle int
		profile  string
	}{
		{"an ASS stream", assStream, webProfile},
		{"a SubRip file", srtFile, webProfile},
		{"none", -1, webProfile},
		{"a PGS stream drawn into HLS", pgsStream, burns},
	} {
		if src := playbackInfo(t, api, film, c.subtitle, c.profile); src["DefaultSubtitleStreamIndex"] != float64(c.subtitle) {
			t.Errorf("%s chosen: the default is %v, want %d", c.name, src["DefaultSubtitleStreamIndex"], c.subtitle)
		}
	}
}
