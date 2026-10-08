package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/httpapi"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/mdblist"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/omdb"
	"github.com/olivertgwalton/photon-server/internal/opensubtitles"
	"github.com/olivertgwalton/photon-server/internal/plugin"
	"github.com/olivertgwalton/photon-server/internal/provider"
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

// metadataProviders runs TMDB before TheTVDB and OMDb, as its match may give them an id to find a
// title by.
func metadataProviders(st *store.Store, plugins *plugin.Plugins, cache *kv.KV) *provider.Registry {
	return provider.NewRegistry(plugins.Load,
		tmdb.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceTMDB)
		}, cache),
		tvdb.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceTVDB)
		}, cache),
		mdblist.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceMDBList)
		}, cache),
		omdb.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceOMDb)
		}, cache),
		opensubtitles.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceOpenSubtitles)
		}, cache),
	)
}

// webApp is the web app's build, beside the binary as an install lays it out
// (/usr/local/share/photon-server/web for /usr/local/bin/photon-server), served wherever there is
// one; nil where there is none, and the server answers the API alone. Its pages may draw from
// where objectOrigin says clients read artwork and previews.
func webApp(objectOrigin func() string) (*httpapi.Web, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	build := os.DirFS(filepath.Join(filepath.Dir(exe), "..", "share", "photon-server", "web"))
	if _, err := fs.Stat(build, "index.html"); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return httpapi.NewWeb(build, objectOrigin)
}

// encoders are what a node may encode video on, in the order it tries them: VideoToolbox on a
// Mac, NVENC on an NVIDIA card, then QSV and VAAPI on each render node (QSV is faster on Intel;
// only VAAPI drives AMD).
func encoders() ([]hls.Hardware, error) {
	var out []hls.Hardware
	if runtime.GOOS == "darwin" {
		out = append(out, hls.Hardware{Accel: domain.AccelVideoToolbox})
	}
	out = append(out, hls.Hardware{Accel: domain.AccelNVENC, Device: "0"})
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

// hardware is the first of the encoders that encodes a test picture, as the node starts, or
// software where none does. HEVC is encoded too where the device, and software for a subtitle
// drawn in, encode it; otherwise H.264 alone, which is reported.
func hardware(ctx context.Context, ffmpeg string, logger *slog.Logger) (hls.Hardware, error) {
	tried, err := encoders()
	if err != nil {
		return hls.Hardware{}, err
	}
	hw := hls.Hardware{Accel: domain.AccelSoftware}
	for _, e := range tried {
		if err := e.Check(ctx, ffmpeg, domain.VideoH264); err != nil {
			logger.DebugContext(ctx, "not encoding on", slog.String("acceleration", string(e.Accel)), slog.String("device", e.Device), slog.Any("err", err))
			continue
		}
		hw = e
		break
	}
	if hw.Accel == domain.AccelSoftware {
		if err := hw.Check(ctx, ffmpeg, domain.VideoH264); err != nil {
			logger.ErrorContext(ctx, "transcoding will fail: ffmpeg would not encode a test picture", slog.Any("err", err))
		}
	}
	hw.HEVC = domain.HEVCAllow
	err = hw.Check(ctx, ffmpeg, domain.VideoHEVC)
	if err == nil && hw.Accel != domain.AccelSoftware {
		err = hls.Hardware{Accel: domain.AccelSoftware}.Check(ctx, ffmpeg, domain.VideoHEVC)
	}
	if err != nil {
		logger.WarnContext(ctx, "encoding H.264 alone", slog.Any("err", err))
		hw.HEVC = domain.HEVCDeny
	}
	logger.InfoContext(ctx, "encoding video", slog.String("on", string(hw.Accel)), slog.String("device", hw.Device), slog.String("hevc", string(hw.HEVC)))
	return hw, nil
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
	case domain.AccelVideoToolbox, domain.AccelVAAPI, domain.AccelQSV, domain.AccelNVENC:
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
