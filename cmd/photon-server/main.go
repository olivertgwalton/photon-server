package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	// The image has no zoneinfo, and the maintenance window is kept in a zone named by an admin.
	_ "time/tzdata"

	"github.com/olivertgwalton/photon-server/internal/analysis"
	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/events"
	"github.com/olivertgwalton/photon-server/internal/historyimport"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/httpapi"
	"github.com/olivertgwalton/photon-server/internal/identify"
	"github.com/olivertgwalton/photon-server/internal/jellyfin"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/nodes"
	"github.com/olivertgwalton/photon-server/internal/peer"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/plugin"
	"github.com/olivertgwalton/photon-server/internal/scan"
	"github.com/olivertgwalton/photon-server/internal/secure"
	"github.com/olivertgwalton/photon-server/internal/storage"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/subtitles"
	"github.com/olivertgwalton/photon-server/internal/task"
	"github.com/olivertgwalton/photon-server/internal/themerr"
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

// exitRestart is the exit code of a node stopped for a restore, which its restart policy starts
// again: EX_TEMPFAIL, a failure, so a policy that restarts on failure alone does too.
const exitRestart = 75

// errRestart is a node stopped for a restore, to be started again.
var errRestart = errors.New("stopped for a restore; to be started again")

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	err := run(logger, os.Args[1:])
	switch {
	case errors.Is(err, errRestart):
		logger.Info("photon-server stopped for a restore; its restart policy starts it again", slog.Int("exit", exitRestart))
		os.Exit(exitRestart)
	case err != nil:
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
	case len(args) == 2 && args[0] == "restore":
		valkeyURL, err := requiredEnv("PHOTON_VALKEY_URL")
		if err != nil {
			return err
		}
		return restorer(databaseURL, valkeyURL, logger).Restore(ctx, args[1], os.Stdout)
	}
	return fmt.Errorf("usage: photon-server [migrate | profile | restore <dump> | openapi], got %q", args)
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

// serve runs the node until it is told to stop; stopped for a restore, it restores the dump
// where it keeps it, and answers errRestart, for the restart policy to start it again.
func serve(ctx context.Context, logger *slog.Logger, databaseURL string) error {
	valkeyURL, err := requiredEnv("PHOTON_VALKEY_URL")
	if err != nil {
		return err
	}
	err = serveNode(ctx, logger, databaseURL, valkeyURL)
	stopped, ok := errors.AsType[*stoppedForRestore](err)
	if !ok {
		return err
	}
	if stopped.leads {
		if err := lead(ctx, logger, restorer(databaseURL, valkeyURL, logger), stopped.file, stopped.server, stopped.restore); err != nil {
			return err
		}
	}
	return errRestart
}

func serveNode(ctx context.Context, logger *slog.Logger, databaseURL, valkeyURL string) error {
	started := time.Now()
	tools, err := media.FindTools(ctx)
	if err != nil {
		return err
	}
	logTools(ctx, logger, tools)
	listen := cmp.Or(os.Getenv("PHOTON_LISTEN"), defaultListen)
	underway := func(ctx context.Context) (bool, error) { return restoreUnderway(ctx, databaseURL, valkeyURL) }
	if err := awaitRestore(ctx, listen, underway, logger); err != nil {
		return err
	}
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
	restores := backup.Restores{Restorer: restorer(databaseURL, valkeyURL, logger), Dir: dumper.Dir, Node: node, KV: cache, Raise: hub.Raise}
	// A restore under way stops the node as a signal does, but for its streams, which it ends;
	// the watch outlives what it stops, to keep the restore while this node goes on to restore it.
	ctx, stopFor := context.WithCancelCause(ctx)
	defer stopFor(nil)
	watched := make(chan struct{})
	defer close(watched)
	go watchRestore(context.WithoutCancel(ctx), cache, id, node, dumper.Dir, stopFor, watched, logger)
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
	imports := historyimport.New(st, logger)
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
	sent := playback.NewSent()
	jellyfinAPI := jellyfin.NewListener(st, hub.Subscribe, jellyfin.New(logger, info, jellyfin.Services{
		Auth: authService, Limits: cache, Raise: hub.Raise, Proxies: trusted, Catalogue: st, Pictures: pictureCache,
		Playing: files, Playbacks: sessions, Watching: st, HLS: remuxer, Placer: placer, Owners: owners,
		Signer: signer, Encoding: playback.Encoding{HEVC: hw.HEVC, Libass: tools.Libass}, Network: st, Sent: sent,
	}), listen, secured.Listen, secured.TLSConfig(), logger)
	finished := jobs.NewFinished()
	srv := &http.Server{
		Addr: listen, TLSConfig: secured.TLSConfig(),
		Handler: httpapi.New(logger, info, httpapi.Services{
			Ready: ready(st, cache, self), Auth: authService, Profiles: st, Catalogue: st, Libraries: st, Tasks: scheduler, Jobs: st, Backups: restores, Maintenance: st, NowPlaying: cache, ProfileAdmin: st, Avatars: st, Providers: providers, ProviderSettings: st, Plugins: plugins, Collections: st, Preferences: st, Playlists: st, People: st, PersonDescriber: providers, Editing: st, History: st, Pictures: st, Themes: st, Watching: st, Playing: files, Subtitles: fetcher, Playbacks: sessions, Owners: owners, Placer: placer, NodeKey: nodeKey, HLS: remuxer, Signer: signer, Artwork: pictureCache, Previews: st, PreviewFiles: previews, Downloads: st, Conversions: conversions, Limits: cache, Activity: st, Events: hub, Audience: st, Webhooks: st, Importer: imports, HistoryImports: st, TrustedProxies: trusted, Network: st, Storage: st, Stores: stores, Nodes: st, Secure: secured, Jellyfin: jellyfinAPI, Setup: setup, Postgres: st, Valkey: cache, Web: web,
			Metrics: metrics(version, self, remuxer, sessions, placer, sent, finished, cluster{lead: scheduler, st: st, nodes: cache, self: placer}),
			Sent:    sent,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	// Each kind of job has slots of its own, so a big import's file reading never holds up its
	// matching and a scan never waits behind either.
	scanner := jobs.NewWorker(st, logger, node, scanSlots, map[domain.JobKind]jobs.Handler{
		domain.JobScanLibrary: scanLibrary(st, scan.New(st, tools, logger), hub, logger),
	}, hub, nil, finished)
	matching := map[domain.JobKind]jobs.Handler{
		domain.JobIdentify: identify.Handler(st, providers, pictureCache, domain.LocaleOf(lang), hub.Raise, logger),
	}
	// A node without yt-dlp leaves themes to one with it.
	if tools.YTDLP.Path != "" {
		matching[domain.JobTheme] = themerr.Fetch(st, pictureCache, cache, themerr.DB, tools.YTDLP.Path, tools.FFmpeg.Path, logger)
	}
	matcher := jobs.NewWorker(st, logger, node, identifySlots, matching, hub, nil, finished)
	notifier := jobs.NewWorker(st, logger, node, webhookSlots, map[domain.JobKind]jobs.Handler{
		domain.JobDeliverWebhook: webhook.Deliver(st),
	}, hub, nil, finished)
	importer := jobs.NewWorker(st, logger, node, importSlots, map[domain.JobKind]jobs.Handler{
		domain.JobImportHistory: imports.Run,
	}, hub, nil, finished)
	// Each kind of job that reads media has a worker of its own, so however many of one are queued,
	// the others keep their slot, and each gives way to playback and keeps to its timing's hours.
	reader := func(kind domain.JobKind, h jobs.Handler) *jobs.Worker {
		return jobs.NewWorker(st, logger, node, mediaSlots, map[domain.JobKind]jobs.Handler{kind: h}, hub, gate, finished)
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
	}, hub, jobs.When(self.TakesTranscodes), finished))
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
	err = listenUntilDone(ctx, srv, secured.Listen, func() {
		if _, ok := errors.AsType[*stoppedForRestore](context.Cause(ctx)); ok {
			endStreams(self, remuxer)
			return
		}
		again := make(chan os.Signal, 1)
		signal.Notify(again, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(again)
		drain(context.WithoutCancel(ctx), self, remuxer, again, drainFor, logger)
	})
	if stopped, ok := errors.AsType[*stoppedForRestore](context.Cause(ctx)); ok && err == nil {
		return stopped
	}
	return err
}
