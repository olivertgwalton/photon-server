package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/anidb"
	"github.com/olivertgwalton/photon-server/internal/anilist"
	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/fanart"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/httpapi"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/mdblist"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/omdb"
	"github.com/olivertgwalton/photon-server/internal/opensubtitles"
	"github.com/olivertgwalton/photon-server/internal/plugin"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/radarr"
	"github.com/olivertgwalton/photon-server/internal/riven"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/tmdb"
	"github.com/olivertgwalton/photon-server/internal/tvdb"
)

// restorer restores dumps with the Postgres tools on the PATH.
func restorer(databaseURL, valkeyURL string, logger *slog.Logger) (backup.Restorer, error) {
	pgRestore, err := media.Look("pg_restore")
	if err != nil {
		return backup.Restorer{}, err
	}
	psql, err := media.Look("psql")
	if err != nil {
		return backup.Restorer{}, err
	}
	return backup.Restorer{PGRestore: pgRestore, PSQL: psql, DatabaseURL: databaseURL, ValkeyURL: valkeyURL, Log: logger}, nil
}

// mediaTools finds the media tools on the PATH.
func mediaTools(ctx context.Context) (media.Tools, error) {
	return media.FindTools(ctx, media.ToolNames{FFmpeg: "ffmpeg", FFprobe: "ffprobe", YTDLP: "yt-dlp"})
}

func logTools(ctx context.Context, logger *slog.Logger, tools media.Tools) {
	logger.InfoContext(ctx, "media tools",
		slog.String("ffmpeg", tools.FFmpeg.Path), slog.String("ffmpeg_version", tools.FFmpeg.Version),
		slog.String("ffprobe", tools.FFprobe.Path), slog.String("ffprobe_version", tools.FFprobe.Version),
		slog.String("yt_dlp", tools.YTDLP.Path), slog.String("yt_dlp_version", tools.YTDLP.Version))
	if !tools.Chromaprint {
		logger.WarnContext(ctx, "intros and credits are found from chapters only: ffmpeg has no chromaprint muxer")
	}
	if !tools.Libass {
		logger.WarnContext(ctx, "styled subtitles play only on clients that draw them: ffmpeg has no libass")
	}
}

// metadataProviders runs TMDB before TheTVDB, AniList, AniDB, OMDb and fanart.tv, as its match may
// give them an id to find a title by.
func metadataProviders(st *store.Store, plugins *plugin.Plugins, cache *kv.KV) *provider.Registry {
	return provider.NewRegistry(plugins.Load,
		tmdb.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceTMDB)
		}, cache),
		tvdb.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceTVDB)
		}, cache),
		anilist.New(st, cache),
		anidb.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceAniDB)
		}, st, cache),
		mdblist.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceMDBList)
		}, cache),
		omdb.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceOMDb)
		}, cache),
		fanart.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceFanart)
		}, cache),
		opensubtitles.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceOpenSubtitles)
		}, cache),
		riven.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceRiven)
		}),
		radarr.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceRadarr)
		}),
	)
}

// webApp is the web app's build, served wherever there is one; nil where there is none, and the
// server answers the API alone. Its pages may draw from where objectOrigin says clients read
// artwork and previews, and play streams from the nodes' public and local URLs, as each tells the
// others.
func webApp(objectOrigin func() string, cache *kv.KV, logger *slog.Logger) (*httpapi.Web, error) {
	build, err := installed("web")
	if build == nil || err != nil {
		return nil, err
	}
	return httpapi.NewWeb(build, objectOrigin, func(ctx context.Context) []string {
		nodes, err := cache.Nodes(ctx)
		if err != nil {
			logger.WarnContext(ctx, "the nodes' public URLs not read for the web app's policy", slog.Any("err", err))
		}
		var origins []string
		for _, n := range nodes {
			for _, u := range []string{n.PublicURL, n.LocalURL} {
				if u != "" && !slices.Contains(origins, u) {
					origins = append(origins, u)
				}
			}
		}
		return origins
	})
}

// installed is the web app named in share/photon-server beside the binary, as an install lays it
// out (/usr/local/share/photon-server/web for /usr/local/bin/photon-server); nil where there is no
// index.html in it.
func installed(name string) (fs.FS, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir := os.DirFS(filepath.Join(filepath.Dir(exe), "..", "share", "photon-server", name))
	if _, err := fs.Stat(dir, "index.html"); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return dir, nil
}

// encoders are what a node may encode video on, in the order it tries them: VideoToolbox on a
// Mac, NVENC on an NVIDIA card, then QSV and VAAPI on each render node (QSV is faster on Intel;
// only VAAPI drives AMD), or on Windows QSV on an Intel GPU and AMF on an AMD one.
func encoders() ([]hls.Hardware, error) {
	var out []hls.Hardware
	if runtime.GOOS == "darwin" {
		out = append(out, hls.Hardware{Accel: domain.AccelVideoToolbox})
	}
	out = append(out, hls.Hardware{Accel: domain.AccelNVENC, Device: "0"})
	if runtime.GOOS == "windows" {
		return append(out, hls.Hardware{Accel: domain.AccelQSV}, hls.Hardware{Accel: domain.AccelAMF}), nil
	}
	renderNodes, err := filepath.Glob("/dev/dri/renderD*")
	if err != nil {
		return nil, err
	}
	for _, accel := range []domain.Acceleration{domain.AccelQSV, domain.AccelVAAPI} {
		for _, device := range renderNodes {
			out = append(out, hls.Hardware{Accel: accel, Device: device})
		}
	}
	return out, nil
}

// hardware is the first of the encoders choice allows that encodes a test picture, or software
// where none does; one an admin set that encodes none is reported, and the first of all that does
// taken instead. HEVC is encoded too where the device encodes it; otherwise H.264 alone, which is
// reported.
func hardware(ctx context.Context, ffmpeg string, choice domain.EncoderChoice, logger *slog.Logger) (hls.Hardware, error) {
	all, err := encoders()
	if err != nil {
		return hls.Hardware{}, err
	}
	tried := all
	switch choice.Source {
	case domain.EncoderSet:
		tried = chosen(all, choice)
	case domain.EncoderAutomatic:
	}
	hw, found := first(ctx, ffmpeg, tried, logger)
	if !found && choice.Source == domain.EncoderSet && choice.Acceleration != domain.AccelSoftware {
		logger.ErrorContext(ctx, "the encoder set encodes no test picture: encoding on the first that does",
			slog.String("acceleration", string(choice.Acceleration)), slog.String("device", choice.Device))
		hw, _ = first(ctx, ffmpeg, all, logger)
	}
	if hw.Accel == domain.AccelSoftware {
		if err := hw.Check(ctx, ffmpeg, domain.VideoH264); err != nil {
			logger.ErrorContext(ctx, "transcoding will fail: ffmpeg would not encode a test picture", slog.Any("err", err))
		}
	}
	hw.HEVC = domain.HEVCAllow
	if err := hw.Check(ctx, ffmpeg, domain.VideoHEVC); err != nil {
		logger.WarnContext(ctx, "encoding H.264 alone", slog.Any("err", err))
		hw.HEVC = domain.HEVCDeny
	}
	logger.InfoContext(ctx, "encoding video", slog.String("on", string(hw.Accel)), slog.String("device", hw.Device), slog.String("hevc", string(hw.HEVC)))
	if hw.Accel == domain.AccelVAAPI {
		hw.Overlay = hls.ProbeOverlay(ctx, ffmpeg, hw.Device)
	}
	return burning(ctx, ffmpeg, hw, logger), nil
}

// burning is hw with an overlay that burns a test subtitle in. VAAPI's driver's own, or Vulkan's,
// that will not is given up for the CPU's. Any other device has one overlay; one that will not is
// reported, and the device kept, as it still encodes all a subtitle is not burned into.
func burning(ctx context.Context, ffmpeg string, hw hls.Hardware, logger *slog.Logger) hls.Hardware {
	for {
		err := hw.CheckBurn(ctx, ffmpeg)
		switch {
		case err == nil:
			if hw.Accel == domain.AccelVAAPI {
				logger.InfoContext(ctx, "burning subtitles in on vaapi", slog.String("overlay", string(hw.Overlay)))
			}
			return hw
		case hw.Accel == domain.AccelVAAPI && hw.Overlay != hls.OverlayCPU:
			logger.WarnContext(ctx, "burning subtitles in on vaapi through the cpu instead", slog.String("overlay", string(hw.Overlay)), slog.Any("err", err))
			hw.Overlay = hls.OverlayCPU
		default:
			logger.ErrorContext(ctx, "burning subtitles in will fail: ffmpeg would not burn one into a test picture", slog.Any("err", err))
			return hw
		}
	}
}

// chosen are the encoders of all on the acceleration choice sets, on its device where it names
// one, which may be one not tried of itself, as a second NVIDIA card is not; none where it sets
// software.
func chosen(all []hls.Hardware, choice domain.EncoderChoice) []hls.Hardware {
	if choice.Acceleration == domain.AccelSoftware {
		return nil
	}
	out := slices.DeleteFunc(slices.Clone(all), func(h hls.Hardware) bool {
		return h.Accel != choice.Acceleration || choice.Device != "" && h.Device != choice.Device
	})
	if len(out) == 0 && choice.Device != "" {
		out = []hls.Hardware{{Accel: choice.Acceleration, Device: choice.Device}}
	}
	return out
}

// first is the first of tried that encodes a test picture, or software, false, where none does.
func first(ctx context.Context, ffmpeg string, tried []hls.Hardware, logger *slog.Logger) (hls.Hardware, bool) {
	for _, e := range tried {
		if err := e.Check(ctx, ffmpeg, domain.VideoH264); err != nil {
			logger.DebugContext(ctx, "not encoding on", slog.String("acceleration", string(e.Accel)), slog.String("device", e.Device), slog.Any("err", err))
			continue
		}
		return e, true
	}
	return hls.Hardware{Accel: domain.AccelSoftware}, false
}

const (
	// cpusPerTranscode is the logical CPUs one software transcode is given: x264 at veryfast takes
	// about two cores, four hyperthreads, to encode 1080p in real time, and more from a 4K or tone
	// mapped source.
	cpusPerTranscode = 4
	// hardwareTranscodes is the NVENC sessions a GeForce card's driver allowed at once before
	// 591.44 (December 2025), which allows 12 per machine: older drivers still stop at 8, and the
	// other encoders, bound by their throughput rather than a count, reach it at about 1080p too.
	hardwareTranscodes = 8
)

// automaticTranscodes is how many videos a node encodes at once where an admin sets no limit: what
// the device it encodes on keeps up with.
func automaticTranscodes(accel domain.Acceleration) int {
	switch accel {
	case domain.AccelSoftware:
		return max(runtime.NumCPU()/cpusPerTranscode, 1)
	case domain.AccelVideoToolbox, domain.AccelVAAPI, domain.AccelQSV, domain.AccelNVENC, domain.AccelAMF:
	}
	return hardwareTranscodes
}

// nodeID is this node's id among the cluster's, kept in its cache folder beside what is filed
// under it: the conversions it holds and the playbacks it serves. It lasts as long as that folder
// does, so a restart keeps them, and a node given an empty folder is a new node.
func nodeID(dir string) (uuid.UUID, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return uuid.UUID{}, err
	}
	defer root.Close()
	b, err := root.ReadFile("node")
	if err == nil {
		return uuid.Parse(strings.TrimSpace(string(b)))
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return uuid.UUID{}, err
	}
	id := uuid.NewV7()
	// Written whole before it is read: a node stopped halfway must not lose its id to half a file.
	if err := root.WriteFile("node.part", []byte(id.String()+"\n"), 0o600); err != nil {
		return uuid.UUID{}, err
	}
	return id, root.Rename("node.part", "node")
}
