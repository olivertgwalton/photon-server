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

// pgsStream and assStream are the subtitle streams of the film aSubtitledFilm makes.
const pgsStream, assStream = 2, 3

// aSubtitledFilm is a film in an MKV of H.264 and AAC carrying PGS and ASS, served by an API that
// reads its subtitles out with FFmpeg, and its copy.
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
		{ContentKey: []byte("akira"), Parts: []store.Part{part}},
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

// playbackInfo is what PlaybackInfo answers a Jellyfin web app of a profile for a film.
func playbackInfo(t *testing.T, api http.Handler, film uuid.UUID, profile string) map[string]any {
	t.Helper()
	w := serve(api, http.MethodPost, "/Items/"+guid(film)+"/PlaybackInfo", jellyfinWeb,
		`{"SubtitleStreamIndex":`+strconv.Itoa(pgsStream)+`,"DeviceProfile":`+profile+`}`)
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
	src := playbackInfo(t, api, film, `{}`)
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
		{"Format": "ssa", "Method": "External"}, {"Format": "pgssub", "Method": "External"}]
}`

// An app that draws PGS itself beside the video plays a file carrying it as it is, the stream
// read out for it at the address PlaybackInfo gives it, as Jellyfin's DeliveryUrl; an app that
// does not is sent HLS with the subtitle drawn in.
func TestAnAppThatDrawsPGSItselfPlaysTheFileAsItIs(t *testing.T) {
	api, film, copyID := aSubtitledFilm(t)
	src := playbackInfo(t, api, film, webProfile)
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

	src = playbackInfo(t, api, film, strings.Replace(webProfile, `{"Format": "pgssub", "Method": "External"}`, `{"Format": "pgssub", "Method": "Encode"}`, 1))
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
	if pgs := subtitleStream(t, src, pgsStream); pgs["DeliveryMethod"] != nil {
		t.Errorf("the PGS stream drawn in: %v", pgs)
	}
}
