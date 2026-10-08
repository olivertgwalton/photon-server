package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
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
		tmdb.New(cmp.Or(os.Getenv("PHOTON_TMDB_TOKEN"), tmdb.DefaultToken), cache),
		tvdb.New(cmp.Or(os.Getenv("PHOTON_TVDB_KEY"), tvdb.DefaultKey), os.Getenv("PHOTON_TVDB_PIN"), cache),
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

// hardware is the device PHOTON_HWACCEL names to encode on (software when unset), on
// PHOTON_HWACCEL_DEVICE: a render node for VAAPI and QSV, a CUDA index for NVENC. A device that
// will not encode is reported and passed over for software, as playing slowly beats not playing.
// HEVC is encoded unless PHOTON_HEVC_ENCODING is deny, or the device, or software for a subtitle
// drawn in, will not encode it, which is reported and H.264 encoded alone.
func hardware(ctx context.Context, ffmpeg string, logger *slog.Logger) (hls.Hardware, error) {
	accel, err := domain.Parse("acceleration", cmp.Or(os.Getenv("PHOTON_HWACCEL"), string(domain.AccelSoftware)), domain.Accelerations())
	if err != nil {
		return hls.Hardware{}, fmt.Errorf("PHOTON_HWACCEL: %w", err)
	}
	hevc, err := domain.Parse("HEVC encoding", cmp.Or(os.Getenv("PHOTON_HEVC_ENCODING"), string(domain.HEVCAllow)), domain.HEVCEncodings())
	if err != nil {
		return hls.Hardware{}, fmt.Errorf("PHOTON_HEVC_ENCODING: %w", err)
	}
	device := os.Getenv("PHOTON_HWACCEL_DEVICE")
	switch accel {
	case domain.AccelVAAPI, domain.AccelQSV:
		device = cmp.Or(device, "/dev/dri/renderD128")
	case domain.AccelNVENC:
		device = cmp.Or(device, "0")
	case domain.AccelSoftware, domain.AccelVideoToolbox:
	}
	hw := hls.Hardware{Accel: accel, Device: device}
	if err := hw.Check(ctx, ffmpeg, domain.VideoH264); err != nil {
		if accel == domain.AccelSoftware {
			logger.ErrorContext(ctx, "transcoding will fail: ffmpeg would not encode a test picture", slog.Any("err", err))
		} else {
			logger.WarnContext(ctx, "encoding in software", slog.Any("err", err))
			hw = hls.Hardware{Accel: domain.AccelSoftware}
		}
	}
	hw.HEVC = hevc
	switch hevc {
	case domain.HEVCAllow:
		err := hw.Check(ctx, ffmpeg, domain.VideoHEVC)
		if err == nil && hw.Accel != domain.AccelSoftware {
			err = hls.Hardware{Accel: domain.AccelSoftware}.Check(ctx, ffmpeg, domain.VideoHEVC)
		}
		if err != nil {
			logger.WarnContext(ctx, "encoding H.264 alone", slog.Any("err", err))
			hw.HEVC = domain.HEVCDeny
		}
	case domain.HEVCDeny:
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
