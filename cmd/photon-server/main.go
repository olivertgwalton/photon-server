package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/analysis"
	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/discovery"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/events"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/httpapi"
	"github.com/olivertgwalton/photon-server/internal/identify"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/mdblist"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/plugin"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/scan"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/task"
	"github.com/olivertgwalton/photon-server/internal/tmdb"
	"github.com/olivertgwalton/photon-server/internal/tvdb"
	"github.com/olivertgwalton/photon-server/internal/watch"
	"github.com/olivertgwalton/photon-server/internal/webhook"
)

// version is stamped by the image's build: -ldflags "-X main.version=…".
var version = "(devel)"

const (
	defaultListen = ":8640"
	shutdownGrace = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("photon-server stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL, err := requiredEnv("PHOTON_DATABASE_URL")
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0:
		return serve(ctx, logger, databaseURL)
	case len(args) == 1 && args[0] == "migrate":
		return store.Migrate(ctx, databaseURL, logger)
	case args[0] == "library":
		return library(ctx, logger, databaseURL, os.Stdout, args[1:])
	case args[0] == "scan":
		return scanLibraries(ctx, logger, databaseURL, os.Stdout, args[1:])
	case args[0] == "profile":
		return profileCommand(ctx, logger, databaseURL, os.Stdout, args[1:])
	}
	return fmt.Errorf("usage: photon-server [migrate | library | scan | profile], got %q", args)
}

func requiredEnv(name string) (string, error) {
	if v := os.Getenv(name); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s is not set", name)
}

func serve(ctx context.Context, logger *slog.Logger, databaseURL string) error {
	valkeyURL, err := requiredEnv("PHOTON_VALKEY_URL")
	if err != nil {
		return err
	}
	tools, err := media.FindTools(ctx)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "media tools",
		slog.String("ffmpeg", tools.FFmpeg.Path), slog.String("ffmpeg_version", tools.FFmpeg.Version),
		slog.String("ffprobe", tools.FFprobe.Path), slog.String("ffprobe_version", tools.FFprobe.Version))
	if !tools.Chromaprint {
		logger.WarnContext(ctx, "intros and credits are found from chapters only: ffmpeg has no chromaprint muxer")
	}
	st, err := store.Open(ctx, databaseURL, logger)
	if err != nil {
		return err
	}
	defer st.Close()
	cache, err := kv.Open(valkeyURL)
	if err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	defer cache.Close()
	id, err := st.ServerID(ctx)
	if err != nil {
		return err
	}
	trusted, err := httpapi.ParseTrustedProxies(os.Getenv("PHOTON_TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	cacheRoot := cmp.Or(os.Getenv("PHOTON_CACHE_DIR"), filepath.Join(cacheDir, "photon-server"))
	pictureCache, err := artwork.Open(filepath.Join(cacheRoot, "artwork"))
	if err != nil {
		return err
	}
	defer pictureCache.Close()
	previews, err := analysis.OpenPreviews(filepath.Join(cacheRoot, "previews"))
	if err != nil {
		return err
	}
	defer previews.Close()
	signingKey, err := st.SigningKey(ctx)
	if err != nil {
		return err
	}
	discoveryMode, err := domain.ParseDiscovery(cmp.Or(os.Getenv("PHOTON_DISCOVERY"), string(domain.DiscoveryBroadcast)))
	if err != nil {
		return fmt.Errorf("PHOTON_DISCOVERY: %w", err)
	}
	hw, err := hardware(ctx, tools.FFmpeg.Path, logger)
	if err != nil {
		return err
	}
	transcodes, err := maxTranscodes(hw.Accel)
	if err != nil {
		return err
	}
	remuxer, err := hls.NewRemuxer(tools.FFmpeg.Path, filepath.Join(cacheRoot, "hls"), hw, transcodes, logger)
	if err != nil {
		return err
	}
	authService, err := auth.New(ctx, st, cache)
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	info := httpapi.Info{
		ID:      id.String(),
		Name:    cmp.Or(os.Getenv("PHOTON_NAME"), hostname),
		Version: version,
	}
	// node is this process among the cluster's.
	node := uuid.NewV7()
	conversions, err := playback.NewConversions(st, cache, tools.FFmpeg.Path, hw, filepath.Join(cacheRoot, "downloads"), node)
	if err != nil {
		return err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dumper := backup.Dumper{
		PGDump: cmp.Or(os.Getenv("PHOTON_PG_DUMP"), "pg_dump"), URL: databaseURL,
		Dir: cmp.Or(os.Getenv("PHOTON_BACKUP_DIR"), filepath.Join(configDir, "photon-server", "backups")),
	}
	hub := events.New(st, cache, events.Server{ID: id, Name: info.Name}, logger)
	scheduler := task.NewScheduler(st, logger, node, hub.Raise, scanTask(st), sweepTask(st, logger), backupTask(dumper, hub, logger), refreshTask(st, logger), sweepArtworkTask(st, pictureCache, logger), markersTask(st, tools, logger), previewsTask(st, previews, logger), sweepDownloadsTask(st, logger), pruneActivityTask(st, logger))
	lang := cmp.Or(os.Getenv("PHOTON_METADATA_LANGUAGE"), "en-US")
	plugins := plugin.New(st)
	// TMDB runs before TheTVDB, as its match may give TheTVDB an id to find a show by.
	providers := provider.NewRegistry(plugins.Load,
		tmdb.New(cmp.Or(os.Getenv("PHOTON_TMDB_TOKEN"), tmdb.DefaultToken), lang, cache),
		tvdb.New(cmp.Or(os.Getenv("PHOTON_TVDB_KEY"), tvdb.DefaultKey), os.Getenv("PHOTON_TVDB_PIN"), lang, cache),
		mdblist.New(func(ctx context.Context) (map[string]string, error) {
			return st.ProviderSettings(ctx, domain.SourceMDBList)
		}, cache),
	)
	srv := &http.Server{
		Addr: cmp.Or(os.Getenv("PHOTON_LISTEN"), defaultListen),
		Handler: httpapi.New(logger, info, httpapi.Services{
			Ready: ready(st, cache), Auth: authService, Profiles: st, Catalogue: st, Libraries: st, Tasks: scheduler, Jobs: st, NowPlaying: cache, ProfileAdmin: st, Providers: providers, ProviderSettings: st, Plugins: plugins, Collections: st, Playlists: st, People: st, PersonDescriber: providers, Editing: st, History: st, Pictures: st, Watching: st, Playing: st, Playbacks: playback.NewSessions(cache, st, remuxer.Close, hub.Raise, node), Owners: playback.NewRouter(cache, node), Remuxing: playback.NewRemuxes(st, tools, remuxer), HLS: remuxer, Signer: playback.NewSigner(signingKey), Artwork: pictureCache, Previews: st, PreviewFiles: previews, Downloads: st, Conversions: conversions, Limits: cache, Activity: st, Events: hub, TrustedProxies: trusted,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	worker := jobs.NewWorker(st, logger, node, max(runtime.NumCPU()/2, 1), map[domain.JobKind]jobs.Handler{
		domain.JobKeyframes:      analysis.Keyframes(st, tools),
		domain.JobIdentify:       identify.Handler(st, providers, logger),
		domain.JobScanLibrary:    scanLibrary(st, scan.New(st, tools, logger), hub, logger),
		domain.JobMarkers:        analysis.Markers(st, tools.Fingerprint),
		domain.JobDeliverWebhook: webhook.Deliver(st),
	}, hub.Raise)
	// Previews have a worker and a slot of their own, so however many are queued, the other jobs
	// keep every slot of theirs.
	previewer := jobs.NewWorker(st, logger, node, 1, map[domain.JobKind]jobs.Handler{
		domain.JobPreviews: analysis.MakePreviews(st, tools, previews, logger),
	}, hub.Raise)
	// Conversions have slots of their own, so a long one never holds up a scan, and a node's are
	// few, so they never starve its playbacks.
	converter := jobs.NewWorker(st, logger, node, playback.MaxConversions, map[domain.JobKind]jobs.Handler{
		domain.JobConvert: conversions.Convert,
	}, hub.Raise)
	watcher := watch.New(st, logger)
	background, stopBackground := context.WithCancel(ctx)
	var wg sync.WaitGroup
	// On the signal's context, not background's, so its streams end before Shutdown waits on them.
	wg.Go(func() { hub.Run(ctx) })
	wg.Go(func() { scheduler.Run(background) })
	wg.Go(func() { worker.Run(background) })
	wg.Go(func() { previewer.Run(background) })
	wg.Go(func() { converter.Run(background) })
	wg.Go(func() { sweepRemuxes(background, remuxer) })
	wg.Go(func() { pruneConversions(background, conversions, logger) })
	if address := os.Getenv("PHOTON_NODE_ADDRESS"); address != "" {
		wg.Go(func() { advertise(background, cache, node, address, logger) })
	}
	wg.Go(func() {
		if err := watcher.Run(background); err != nil {
			logger.WarnContext(ctx, "libraries are scanned on schedule only", slog.Any("err", err))
		}
	})
	switch discoveryMode {
	case domain.DiscoveryBroadcast:
		wg.Go(func() { answerDiscovery(background, srv.Addr, info, logger) })
	case domain.DiscoveryOff:
	}
	defer func() {
		stopBackground()
		wg.Wait()
	}()

	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe() }()
	logger.InfoContext(ctx, "serving", slog.String("addr", srv.Addr), slog.String("version", info.Version))

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func ready(st *store.Store, cache *kv.KV) func(context.Context) error {
	return func(ctx context.Context) error {
		var errs []error
		if err := st.Ping(ctx); err != nil {
			errs = append(errs, fmt.Errorf("postgres: %w", err))
		}
		if err := cache.Ping(ctx); err != nil {
			errs = append(errs, fmt.Errorf("valkey: %w", err))
		}
		return errors.Join(errs...)
	}
}

// answerDiscovery answers clients looking for the server on UDP at the HTTP listener's port.
// Clients can still be given the address, so a port it cannot have is only a warning.
func answerDiscovery(ctx context.Context, addr string, info httpapi.Info, logger *slog.Logger) {
	conn, err := new(net.ListenConfig).ListenPacket(ctx, "udp", addr)
	if err == nil {
		err = discovery.Serve(ctx, conn, info, logger)
	}
	if err != nil {
		logger.WarnContext(ctx, "clients must be given the server's address", slog.Any("err", err))
	}
}

// sweepRemuxes ends the remuxes of players that went away without stopping.
// hardware is the device PHOTON_HWACCEL names to encode on (software when unset), on
// PHOTON_HWACCEL_DEVICE: a render node for VAAPI and QSV, a CUDA index for NVENC. A device that
// will not encode is reported and passed over for software, as playing slowly beats not playing.
func hardware(ctx context.Context, ffmpeg string, logger *slog.Logger) (hls.Hardware, error) {
	accel, ok := domain.ParseAcceleration(cmp.Or(os.Getenv("PHOTON_HWACCEL"), string(domain.AccelSoftware)))
	if !ok {
		return hls.Hardware{}, fmt.Errorf("PHOTON_HWACCEL is software, videotoolbox, vaapi, qsv or nvenc, not %q", os.Getenv("PHOTON_HWACCEL"))
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
	if err := hw.Check(ctx, ffmpeg); err != nil {
		if accel == domain.AccelSoftware {
			logger.ErrorContext(ctx, "transcoding will fail: ffmpeg would not encode a test picture", slog.Any("err", err))
			return hw, nil
		}
		logger.WarnContext(ctx, "encoding in software", slog.Any("err", err))
		return hls.Hardware{Accel: domain.AccelSoftware}, nil
	}
	logger.InfoContext(ctx, "encoding video", slog.String("on", string(accel)), slog.String("device", device))
	return hw, nil
}

const (
	// cpusPerTranscode is the logical CPUs one software transcode is given: x264 at veryfast takes
	// about two cores, four hyperthreads, to encode 1080p in real time, and more from a 4K or tone
	// mapped source.
	cpusPerTranscode = 4
	// hardwareTranscodes is NVIDIA's cap on NVENC sessions at once on a GeForce card, which the
	// other encoders, bound by their throughput rather than a count, reach at about 1080p too.
	hardwareTranscodes = 8
)

// maxTranscodes is how many videos the node encodes at once: PHOTON_MAX_TRANSCODES, a number or
// unlimited, else what the device it encodes on keeps up with.
func maxTranscodes(accel domain.Acceleration) (int, error) {
	switch v := os.Getenv("PHOTON_MAX_TRANSCODES"); v {
	case "":
	case "unlimited":
		return hls.Unlimited, nil
	default:
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return 0, fmt.Errorf("PHOTON_MAX_TRANSCODES is a positive number or unlimited, not %q", v)
		}
		return n, nil
	}
	switch accel {
	case domain.AccelSoftware:
		return max(runtime.NumCPU()/cpusPerTranscode, 1), nil
	case domain.AccelVideoToolbox, domain.AccelVAAPI, domain.AccelQSV, domain.AccelNVENC:
	}
	return hardwareTranscodes, nil
}

// advertiseEvery is how often a node says where its peers reach it; it is forgotten after three
// times that, quiet.
const advertiseEvery = 15 * time.Second

// advertise says where this node's peers reach it, PHOTON_NODE_ADDRESS, so a request for HLS one
// of its playbacks makes is handed to it whichever node it lands on.
func advertise(ctx context.Context, cache *kv.KV, node uuid.UUID, address string, logger *slog.Logger) {
	t := time.NewTicker(advertiseEvery)
	defer t.Stop()
	for {
		if err := cache.SetNode(ctx, node, address, 3*advertiseEvery); err != nil && ctx.Err() == nil {
			logger.WarnContext(ctx, "node address not advertised", slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func sweepRemuxes(ctx context.Context, r *hls.Remuxer) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Sweep()
		}
	}
}

// pruneEvery is how often a node removes the converted files no download needs any more.
const pruneEvery = 10 * time.Minute

// pruneConversions prunes at start, which clears what a node stopped mid-conversion left, and
// every pruneEvery after.
func pruneConversions(ctx context.Context, c *playback.Conversions, logger *slog.Logger) {
	t := time.NewTicker(pruneEvery)
	defer t.Stop()
	for {
		if err := c.Prune(ctx); err != nil && ctx.Err() == nil {
			logger.WarnContext(ctx, "converted files not pruned", slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
