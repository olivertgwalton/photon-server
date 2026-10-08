package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	// The image has no zoneinfo, and the maintenance window is kept in a zone named by an admin.
	_ "time/tzdata"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/analysis"
	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/discovery"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/events"
	"github.com/olivertgwalton/photon-server/internal/historyimport"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/httpapi"
	"github.com/olivertgwalton/photon-server/internal/identify"
	"github.com/olivertgwalton/photon-server/internal/jellyfin"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/mdblist"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/nodes"
	"github.com/olivertgwalton/photon-server/internal/omdb"
	"github.com/olivertgwalton/photon-server/internal/opensubtitles"
	"github.com/olivertgwalton/photon-server/internal/peer"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/plugin"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/scan"
	"github.com/olivertgwalton/photon-server/internal/secure"
	"github.com/olivertgwalton/photon-server/internal/storage"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/subtitles"
	"github.com/olivertgwalton/photon-server/internal/task"
	"github.com/olivertgwalton/photon-server/internal/themerr"
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
	// drainFor is the longest a node stopping plays its streams to their end: a film's length.
	drainFor = 2 * time.Hour
	// drainPoll is how often a node draining looks whether its streams have ended.
	drainPoll = 5 * time.Second
)

const (
	// scanSlots is how many libraries a node scans at once, each scan being one library's: a
	// films library and a shows library together, which are usually on the same disk or mount,
	// so a third would only share its reads.
	scanSlots = 2
	// identifySlots is how many titles a node matches at once. A match spends its time waiting
	// on providers, a request at a time, about two seconds of it a title (measured against TMDB),
	// so sixteen at once reach TMDB's rate limit, which every node shares; past it a match waits
	// on the limit, not the network.
	identifySlots = 16
	// webhookSlots is how many deliveries a node makes at once, so one receiver that does not
	// answer holds up no other.
	webhookSlots = 2
	// importSlots is how many history imports a node runs at once, each a source's whole library
	// read a page at a time.
	importSlots = 1
	// mediaSlots is how many jobs of each kind that reads media a node runs at once: one, as
	// Jellyfin's chapter images and trickplay go through files one by one and Plex's butler file by
	// file. A still is a seek into the whole file, and on a network mount ten at once took every
	// byte a stream needed (measured: every chapter timed out, and 64 MiB took minutes to read).
	mediaSlots = 1
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

	if len(args) == 1 && args[0] == "openapi" {
		return writeDescription(os.Stdout)
	}
	databaseURL, err := requiredEnv("PHOTON_DATABASE_URL")
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0:
		return serve(ctx, logger, databaseURL)
	case len(args) == 1 && args[0] == "migrate":
		return store.Migrate(ctx, databaseURL, logger)
	case args[0] == "profile":
		return profileCommand(ctx, logger, databaseURL, os.Stdout, args[1:])
	}
	return fmt.Errorf("usage: photon-server [migrate | profile | openapi], got %q", args)
}

// writeDescription writes the API's OpenAPI description, so a client's types are generated
// without a server or database.
func writeDescription(w io.Writer) error {
	doc, err := httpapi.Describe(domain.Info{Version: version})
	if err != nil {
		return err
	}
	_, err = w.Write(append(doc, '\n'))
	return err
}

func requiredEnv(name string) (string, error) {
	if v := os.Getenv(name); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s is not set", name)
}

func serve(ctx context.Context, logger *slog.Logger, databaseURL string) error {
	started := time.Now()
	valkeyURL, err := requiredEnv("PHOTON_VALKEY_URL")
	if err != nil {
		return err
	}
	tools, err := media.FindTools(ctx)
	if err != nil {
		return err
	}
	logTools(ctx, logger, tools)
	st, err := store.Open(ctx, databaseURL, logger)
	if err != nil {
		return err
	}
	defer st.Close()
	id, err := st.ServerID(ctx)
	if err != nil {
		return err
	}
	cache, err := kv.Open(valkeyURL, id)
	if err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	defer cache.Close()
	trusted, err := peer.Parse(os.Getenv("PHOTON_TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	public, err := httpapi.ParsePublicURL(os.Getenv("PHOTON_PUBLIC_URL"))
	if err != nil {
		return err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	cacheRoot := cmp.Or(os.Getenv("PHOTON_CACHE_DIR"), filepath.Join(cacheDir, "photon-server"))
	stores, err := storage.Open(ctx, st, cacheRoot, logger)
	if err != nil {
		return err
	}
	defer stores.Close()
	pictureCache := artwork.New(&stores.Artwork, st.SetBlurhash)
	previews := analysis.NewPreviews(&stores.Previews)
	web, err := webApp(stores.Origin)
	if err != nil {
		return err
	}
	signingKey, err := st.SigningKey(ctx)
	if err != nil {
		return err
	}
	discoveryMode, err := domain.Parse("discovery", cmp.Or(os.Getenv("PHOTON_DISCOVERY"), string(domain.DiscoveryBroadcast)), domain.Discoveries())
	if err != nil {
		return fmt.Errorf("PHOTON_DISCOVERY: %w", err)
	}
	hw, err := hardware(ctx, tools.FFmpeg.Path, logger)
	if err != nil {
		return err
	}
	automatic := automaticTranscodes(hw.Accel)
	remuxer, err := hls.NewRemuxer(tools, filepath.Join(cacheRoot, "hls"), filepath.Join(cacheRoot, "subtitles"), hw, automatic, logger)
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
	node, err := nodeID(cacheRoot)
	if err != nil {
		return err
	}
	address := os.Getenv("PHOTON_NODE_ADDRESS")
	// self is this node, doing what an admin sets of it: its role, and how many videos it encodes.
	self, err := nodes.Join(ctx, st, node, hostname, address,
		domain.Encoder{Acceleration: hw.Accel, HEVC: hw.HEVC, Libass: tools.Libass}, automatic, remuxer, logger)
	if err != nil {
		return err
	}
	info := domain.Info{
		ID:      id.String(),
		Name:    cmp.Or(os.Getenv("PHOTON_NAME"), hostname),
		Version: version,
	}
	conversions, err := playback.NewConversions(st, cache, remuxer, tools.FFmpeg.Path, hw, filepath.Join(cacheRoot, "downloads"), node)
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
	gate := jobs.NewGate(cache, st, hub.Subscribe, logger)
	window := task.Trigger{Kind: task.TriggerWindow, Opens: gate.Opens}
	plugins := plugin.New(st)
	providers := metadataProviders(st, plugins, cache)
	fetcher := subtitles.NewFetcher(st, providers)
	scheduler := task.NewScheduler(st, logger, node, hub.Raise, scanTask(st), sweepTask(st, logger), backupTask(dumper, hub, logger), refreshTask(st, logger), sweepArtworkTask(st, pictureCache, logger), markersTask(st, tools, window, logger), previewsTask(st, previews, window, logger), sweepDownloadsTask(st, logger), pruneActivityTask(st, logger), refreshCollectionsTask(st), syncListsTask(st, providers), fetchSubtitlesTask(fetcher, logger))
	lang := cmp.Or(os.Getenv("PHOTON_METADATA_LANGUAGE"), "en-US")
	_, country, _ := strings.Cut(lang, "-")
	if err := st.SetCertificateCountry(ctx, country); err != nil {
		return err
	}
	sessions := playback.NewSessions(cache, st, remuxer, hub.Raise, node)
	imports := historyimport.New(st)
	listen := cmp.Or(os.Getenv("PHOTON_LISTEN"), defaultListen)
	setup := httpapi.Setup{
		Started: started, Node: node, Listen: listen, Tools: tools, Encoder: hw, Discovery: discoveryMode,
		MetadataLanguage: lang, CacheDir: cacheRoot, BackupDir: dumper.Dir, PublicURL: public,
	}
	// Fetched subtitles are written from Postgres into each node's cache as it opens them.
	files, err := subtitles.NewFiles(st, filepath.Join(cacheRoot, "fetched-subtitles"))
	if err != nil {
		return err
	}
	owners, remuxes, signer := playback.NewRouter(cache, node), playback.NewRemuxes(files, remuxer), playback.NewSigner(signingKey)
	nodeKey, err := nodecall.NewKey(signingKey)
	if err != nil {
		return err
	}
	placer := playback.NewPlacer(cache, self.Node, remuxes, nodeKey)
	secured := secure.New(st, hub.Subscribe, logger)
	jellyfinAPI := jellyfin.NewListener(st, hub.Subscribe, jellyfin.New(logger, info, jellyfin.Services{
		Auth: authService, Limits: cache, Raise: hub.Raise, Proxies: trusted, Catalogue: st, Pictures: pictureCache,
		Playing: files, Playbacks: sessions, Watching: st, HLS: remuxer, Placer: placer, Owners: owners,
		Signer: signer, Encoding: playback.Encoding{HEVC: hw.HEVC, Libass: tools.Libass}, Network: st,
	}), listen, secured.Listen, secured.TLSConfig(), logger)
	srv := &http.Server{
		Addr: listen, TLSConfig: secured.TLSConfig(),
		Handler: httpapi.New(logger, info, httpapi.Services{
			Ready: ready(st, cache, self), Auth: authService, Profiles: st, Catalogue: st, Libraries: st, Tasks: scheduler, Jobs: st, Maintenance: st, NowPlaying: cache, ProfileAdmin: st, Avatars: st, Providers: providers, ProviderSettings: st, Plugins: plugins, Collections: st, Preferences: st, Playlists: st, People: st, PersonDescriber: providers, Editing: st, History: st, Pictures: st, Themes: st, Watching: st, Playing: files, Subtitles: fetcher, Playbacks: sessions, Owners: owners, Placer: placer, NodeKey: nodeKey, HLS: remuxer, Signer: signer, Artwork: pictureCache, Previews: st, PreviewFiles: previews, Downloads: st, Conversions: conversions, Limits: cache, Activity: st, Events: hub, Audience: st, Webhooks: st, Importer: imports, HistoryImports: st, TrustedProxies: trusted, Network: st, Storage: st, Stores: stores, Nodes: st, Secure: secured, Jellyfin: jellyfinAPI, Setup: setup, Postgres: st, Valkey: cache, Web: web,
			Metrics: metricsHandler(metrics(version, self), logger),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	// Each kind of job has slots of its own, so a big import's file reading never holds up its
	// matching and a scan never waits behind either.
	scanner := jobs.NewWorker(st, logger, node, scanSlots, map[domain.JobKind]jobs.Handler{
		domain.JobScanLibrary: scanLibrary(st, scan.New(st, tools, logger), hub, logger),
	}, hub, nil)
	matching := map[domain.JobKind]jobs.Handler{
		domain.JobIdentify: identify.Handler(st, providers, pictureCache, domain.LocaleOf(lang), hub.Raise, logger),
	}
	// A node without yt-dlp leaves themes to one with it.
	if tools.YTDLP.Path != "" {
		matching[domain.JobTheme] = themerr.Fetch(st, pictureCache, cache, themerr.DB, tools.YTDLP.Path, tools.FFmpeg.Path, logger)
	}
	matcher := jobs.NewWorker(st, logger, node, identifySlots, matching, hub, nil)
	notifier := jobs.NewWorker(st, logger, node, webhookSlots, map[domain.JobKind]jobs.Handler{
		domain.JobDeliverWebhook: webhook.Deliver(st),
	}, hub, nil)
	importer := jobs.NewWorker(st, logger, node, importSlots, map[domain.JobKind]jobs.Handler{
		domain.JobImportHistory: imports.Run,
	}, hub, nil)
	// Each kind of job that reads media has a worker of its own, so however many of one are queued,
	// the others keep their slot, and each gives way to playback and keeps to its timing's hours.
	reader := func(kind domain.JobKind, h jobs.Handler) *jobs.Worker {
		return jobs.NewWorker(st, logger, node, mediaSlots, map[domain.JobKind]jobs.Handler{kind: h}, hub, gate)
	}
	workers := []*jobs.Worker{
		scanner, matcher, notifier, importer,
		reader(domain.JobKeyframes, analysis.Keyframes(st)),
		reader(domain.JobKeyframeWalk, analysis.WalkKeyframes(st, tools)),
		reader(domain.JobMarkers, analysis.Markers(st, tools.Fingerprint, tools.Shades)),
		reader(domain.JobPreviews, analysis.MakePreviews(st, tools, previews, logger)),
		reader(domain.JobProbe, analysis.Probe(st, tools)),
	}
	// Conversions have slots of their own, so a long one never holds up a scan, and each holds a
	// transcode slot its node's playbacks may take, so they never starve them. A conversion is asked
	// for by someone waiting on it, as a playback is, so it waits for no playback: on a server played
	// every evening, one held back by each would not be ready for days. A node that never encodes
	// leaves them to one that does.
	workers = append(workers, jobs.NewWorker(st, logger, node, playback.MaxConversions, map[domain.JobKind]jobs.Handler{
		domain.JobConvert: conversions.Convert,
	}, hub, jobs.When(self.TakesTranscodes)))
	watcher := watch.New(st, logger)
	// What keeps streams playing (serving, telling the others where this node is, ending what has
	// stopped) outlives the signal, as the node drains; what begins new work stops at it.
	background, stopBackground := context.WithCancel(context.WithoutCancel(ctx))
	var wg sync.WaitGroup
	// On the signal's context, not background's, so its streams end before Shutdown waits on them.
	wg.Go(func() { hub.Run(ctx) })
	wg.Go(func() { scheduler.Run(ctx) })
	wg.Go(func() { gate.Run(background) })
	for _, w := range workers {
		wg.Go(func() { w.Run(ctx) })
	}
	wg.Go(func() { sweepPlaybacks(background, sessions, remuxer, logger) })
	wg.Go(func() { pruneConversions(background, conversions, logger) })
	wg.Go(func() { self.Run(background, hub.Subscribe) })
	if address != "" {
		wg.Go(func() { advertise(background, cache, self.Node, remuxer.Changes(), self.Changes(), logger) })
	}
	wg.Go(func() {
		if err := watcher.Run(ctx); err != nil {
			logger.WarnContext(ctx, "libraries are scanned on schedule only", slog.Any("err", err))
		}
	})
	switch discoveryMode {
	case domain.DiscoveryBroadcast:
		wg.Go(func() { answerDiscovery(background, srv.Addr, secured.Scheme, info, logger) })
	case domain.DiscoveryOff:
	}
	defer func() {
		stopBackground()
		wg.Wait()
	}()

	wg.Go(func() { secured.Run(background) })
	wg.Go(func() {
		stores.Run(background, storage.Cluster{Node: node, Subscribe: hub.Subscribe, Raise: hub.Raise, Nodes: nodeIDs(cache)})
	})
	wg.Go(func() { jellyfinAPI.Run(background) })
	logger.InfoContext(ctx, "serving", slog.String("addr", srv.Addr), slog.String("version", info.Version))
	return listenUntilDone(ctx, srv, secured.Listen, func() {
		again := make(chan os.Signal, 1)
		signal.Notify(again, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(again)
		drain(context.WithoutCancel(ctx), self, remuxer, again, drainFor, logger)
	})
}

// drain stops this node taking new work and plays its streams to their end, for limit at most,
// before it stops serving: another node takes new streams meanwhile. A signal on again stops it
// at once.
func drain(ctx context.Context, self interface{ Stop() }, streams interface{ Playbacks() []uuid.UUID }, again <-chan os.Signal, limit time.Duration, logger *slog.Logger) {
	self.Stop()
	t := time.NewTicker(drainPoll)
	defer t.Stop()
	until := time.After(limit)
	said := -1
	for {
		left := len(streams.Playbacks())
		if left == 0 {
			return
		}
		if left != said {
			logger.InfoContext(ctx, "draining: playing streams to their end; stop again to stop at once", slog.Int("streams", left))
			said = left
		}
		select {
		case <-t.C:
		case <-until:
			return
		case <-again:
			return
		}
	}
}

// listenUntilDone serves until ctx ends and drain returns, then gives open requests shutdownGrace
// to finish.
func listenUntilDone(ctx context.Context, srv *http.Server, either func(net.Listener) net.Listener, drain func()) error {
	served := make(chan error, 1)
	go func() {
		l, err := new(net.ListenConfig).Listen(ctx, "tcp", srv.Addr)
		if err != nil {
			served <- err
			return
		}
		served <- srv.Serve(either(l))
	}()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	drain()
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

// defaultWebDir is where the image puts the web app's build.
const defaultWebDir = "/usr/local/share/photon-server/web"

// webApp is the web app's build in PHOTON_WEB_DIR, served when PHOTON_WEB is serve, as it is by
// default wherever there is a build; nil when the server answers the API alone. Its pages may draw
// from where objectOrigin says clients read artwork and previews.
func webApp(objectOrigin func() string) (*httpapi.Web, error) {
	build := os.DirFS(cmp.Or(os.Getenv("PHOTON_WEB_DIR"), defaultWebDir))
	mode := os.Getenv("PHOTON_WEB")
	if mode == "" {
		mode = string(domain.WebOff)
		if _, err := fs.Stat(build, "index.html"); err == nil {
			mode = string(domain.WebServe)
		}
	}
	web, err := domain.Parse("web", mode, domain.Webs())
	if err != nil {
		return nil, fmt.Errorf("PHOTON_WEB: %w", err)
	}
	switch web {
	case domain.WebServe:
		app, err := httpapi.NewWeb(build, objectOrigin)
		if err != nil {
			return nil, fmt.Errorf("PHOTON_WEB is serve but PHOTON_WEB_DIR has no build: %w", err)
		}
		return app, nil
	case domain.WebOff:
	}
	return nil, nil
}

// ready reports what keeps this node from taking new clients: a backend it cannot reach, or its
// stopping, so a balancer sends new clients to the others while it drains.
func ready(st *store.Store, cache *kv.KV, self interface{ Stopping() bool }) func(context.Context) error {
	return func(ctx context.Context) error {
		if self.Stopping() {
			return domain.ErrStopping
		}
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
func answerDiscovery(ctx context.Context, addr string, scheme func() string, info domain.Info, logger *slog.Logger) {
	conn, err := new(net.ListenConfig).ListenPacket(ctx, "udp", addr)
	if err == nil {
		err = discovery.Serve(ctx, conn, info, scheme, logger)
	}
	if err != nil {
		logger.WarnContext(ctx, "clients must be given the server's address", slog.Any("err", err))
	}
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

// advertiseEvery is how often a node says where its peers reach it; it is forgotten after three
// times that, quiet.
const advertiseEvery = 15 * time.Second

// advertise tells the others where this node's peers reach it, PHOTON_NODE_ADDRESS, so a request
// for HLS one of its playbacks makes is handed to it whichever node it lands on, and how many
// videos it encodes, said again as soon as that changes.
func advertise(ctx context.Context, cache *kv.KV, self func() domain.Node, slots, settings <-chan struct{}, logger *slog.Logger) {
	t := time.NewTicker(advertiseEvery)
	defer t.Stop()
	for {
		if err := cache.SetNode(ctx, self(), 3*advertiseEvery); err != nil && ctx.Err() == nil {
			logger.WarnContext(ctx, "node not advertised", slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-slots:
		case <-settings:
		}
	}
}

// sweepEvery is how often a node ends the playbacks of players that went away without stopping,
// closes its streams of playbacks that have ended, and forgets subtitles no one has read lately.
const sweepEvery = 30 * time.Second

func sweepPlaybacks(ctx context.Context, s *playback.Sessions, r *hls.Remuxer, logger *slog.Logger) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		r.SweepSubtitles()
		if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			logger.WarnContext(ctx, "playbacks not swept", slog.Any("err", err))
		}
	}
}

// nodeIDs answers the ids of the nodes that say where their peers reach them.
func nodeIDs(cache *kv.KV) func(ctx context.Context) ([]uuid.UUID, error) {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		adverts, err := cache.Nodes(ctx)
		ids := make([]uuid.UUID, len(adverts))
		for i, n := range adverts {
			ids[i] = n.ID
		}
		return ids, err
	}
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
