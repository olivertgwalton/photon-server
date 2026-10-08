package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"uuid"

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
	"github.com/olivertgwalton/photon-server/internal/provider"
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

// node is a running node's parts, as serveNode wires them together: join makes those that stand
// before a restore is watched for, wire the rest, and serve runs them.
type node struct {
	logger    *slog.Logger
	st        *store.Store
	cache     *kv.KV
	stores    *storage.Stores
	tools     media.Tools
	server    uuid.UUID
	started   time.Time
	listen    string
	cacheRoot string

	id          uuid.UUID
	info        domain.Info
	self        *nodes.Self
	address     string
	hw          hls.Hardware
	remuxer     *hls.Remuxer
	conversions *playback.Conversions
	auth        *auth.Service
	pictures    *artwork.Cache
	previews    *analysis.Previews
	dumper      backup.Dumper
	hub         *events.Hub
	restores    backup.Restores

	gate      *jobs.Gate
	providers *provider.Registry
	scheduler *task.Scheduler
	sessions  *playback.Sessions
	imports   *historyimport.Imports
	lang      string
	secured   *secure.Server
	jellyfin  *jellyfin.Listener
	finished  *jobs.Finished
	discovery domain.Discovery
	srv       *http.Server
}

// join makes this node one of the cluster's: what it encodes on, who it is, and what it raises
// events and keeps backups with.
func (n *node) join(ctx context.Context, databaseURL, valkeyURL string) error {
	hw, err := hardware(ctx, n.tools.FFmpeg.Path, n.logger)
	if err != nil {
		return err
	}
	automatic := automaticTranscodes(hw.Accel)
	n.hw = hw
	n.remuxer, err = hls.NewRemuxer(n.tools, filepath.Join(n.cacheRoot, "hls"), filepath.Join(n.cacheRoot, "subtitles"), hw, automatic, n.logger)
	if err != nil {
		return err
	}
	if n.auth, err = auth.New(ctx, n.st, n.cache); err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	if n.id, err = nodeID(n.cacheRoot); err != nil {
		return err
	}
	n.address = os.Getenv("PHOTON_NODE_ADDRESS")
	// self is this node, doing what an admin sets of it: its role, and how many videos it encodes.
	n.self, err = nodes.Join(ctx, n.st, n.id, hostname, n.address,
		domain.Encoder{Acceleration: hw.Accel, HEVC: hw.HEVC, Libass: n.tools.Libass}, automatic, n.remuxer, n.logger)
	if err != nil {
		return err
	}
	n.info = domain.Info{ID: n.server.String(), Name: cmp.Or(os.Getenv("PHOTON_NAME"), hostname), Version: version}
	n.conversions, err = playback.NewConversions(n.st, n.cache, n.remuxer, n.tools.FFmpeg.Path, hw, filepath.Join(n.cacheRoot, "downloads"), n.id)
	if err != nil {
		return err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	n.pictures = artwork.New(&n.stores.Artwork, n.st.SetBlurhash)
	n.previews = analysis.NewPreviews(&n.stores.Previews)
	pgDump, err := media.Look(cmp.Or(os.Getenv("PHOTON_PG_DUMP"), "pg_dump"))
	if err != nil {
		return err
	}
	rest, err := restorer(databaseURL, valkeyURL, n.logger)
	if err != nil {
		return err
	}
	n.dumper = backup.Dumper{
		PGDump: pgDump, URL: databaseURL,
		Dir: cmp.Or(os.Getenv("PHOTON_BACKUP_DIR"), filepath.Join(configDir, "photon-server", "backups")),
	}
	n.hub = events.New(n.st, n.cache, events.Server{ID: n.server, Name: n.info.Name}, n.logger)
	n.restores = backup.Restores{Restorer: rest, Dir: n.dumper.Dir, Node: n.id, KV: n.cache, Raise: n.hub.Raise}
	return nil
}

// wire makes what serves clients: the tasks, playback, and the APIs.
func (n *node) wire(ctx context.Context) error {
	st, logger := n.st, n.logger
	n.gate = jobs.NewGate(n.cache, st, n.hub.Subscribe, logger)
	window := task.Trigger{Kind: task.TriggerWindow, Opens: n.gate.Opens}
	plugins := plugin.New(st)
	n.providers = metadataProviders(st, plugins, n.cache)
	fetcher := subtitles.NewFetcher(st, n.providers)
	n.scheduler = task.NewScheduler(st, logger, n.id, n.hub.Raise, scanTask(st), sweepTask(st, logger), backupTask(n.dumper, n.hub, logger),
		refreshTask(st, logger), sweepArtworkTask(st, n.pictures, logger), markersTask(st, n.tools, window, logger),
		previewsTask(st, n.previews, window, logger), sweepDownloadsTask(st, logger), pruneActivityTask(st, logger),
		refreshCollectionsTask(st), syncListsTask(st, n.providers), fetchSubtitlesTask(fetcher, logger))
	n.lang = cmp.Or(os.Getenv("PHOTON_METADATA_LANGUAGE"), "en-US")
	_, country, _ := strings.Cut(n.lang, "-")
	if err := st.SetCertificateCountry(ctx, country); err != nil {
		return err
	}
	n.sessions = playback.NewSessions(n.cache, st, n.remuxer, n.hub.Raise, n.id)
	n.imports = historyimport.New(st, n.logger)
	// Fetched subtitles are written from Postgres into each node's cache as it opens them.
	files, err := subtitles.NewFiles(st, filepath.Join(n.cacheRoot, "fetched-subtitles"))
	if err != nil {
		return err
	}
	signingKey, err := st.SigningKey(ctx)
	if err != nil {
		return err
	}
	nodeKey, err := nodecall.NewKey(signingKey)
	if err != nil {
		return err
	}
	trusted, err := peer.Parse(os.Getenv("PHOTON_TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	p := playing{
		files: files, owners: playback.NewRouter(n.cache, n.id), signer: playback.NewSigner(signingKey), nodeKey: nodeKey,
		placer: playback.NewPlacer(n.cache, n.self.Node, playback.NewRemuxes(files, n.remuxer), nodeKey),
		sent:   playback.NewSent(), trusted: trusted, plugins: plugins, fetcher: fetcher,
	}
	n.secured = secure.New(st, n.hub.Subscribe, logger)
	n.jellyfin, err = jellyfin.NewListener(st, n.hub.Subscribe, jellyfin.New(logger, n.info, jellyfin.Services{
		Auth: n.auth, Limits: n.cache, Raise: n.hub.Raise, Proxies: trusted, Catalogue: st, Preferences: st, Playlists: st,
		Pictures: n.pictures, Playing: files, Playbacks: n.sessions, Watching: st, Themes: st, Previews: st, PreviewFiles: n.previews,
		HLS: n.remuxer, Placer: p.placer, Owners: p.owners, Signer: p.signer,
		Encoding: playback.Encoding{HEVC: n.hw.HEVC, Libass: n.tools.Libass}, Network: st, Sent: p.sent,
	}), n.listen, n.secured.Listen, n.secured.TLSConfig(), logger)
	if err != nil {
		return err
	}
	n.finished = jobs.NewFinished()
	if n.discovery, err = discoveryMode(); err != nil {
		return err
	}
	n.srv, err = n.httpServer(p)
	return err
}

// playing is what wire makes for playback that the API is given too.
type playing struct {
	files   subtitles.Files
	owners  *playback.Router
	signer  playback.Signer
	nodeKey nodecall.Key
	placer  *playback.Placer
	sent    *playback.Sent
	trusted peer.Proxies
	plugins *plugin.Plugins
	fetcher subtitles.Fetcher
}

// httpServer is the server of photon's own API and web app.
func (n *node) httpServer(p playing) (*http.Server, error) {
	public, err := httpapi.ParsePublicURL(os.Getenv("PHOTON_PUBLIC_URL"))
	if err != nil {
		return nil, err
	}
	web, err := webApp(n.stores.Origin)
	if err != nil {
		return nil, err
	}
	setup := httpapi.Setup{
		Started: n.started, Node: n.id, Listen: n.listen, Tools: n.tools, Encoder: n.hw, Discovery: n.discovery,
		MetadataLanguage: n.lang, CacheDir: n.cacheRoot, BackupDir: n.dumper.Dir, PublicURL: public,
	}
	st, cache := n.st, n.cache
	return &http.Server{
		Addr: n.listen, TLSConfig: n.secured.TLSConfig(),
		Handler: httpapi.New(n.logger, n.info, httpapi.Services{
			Ready: ready(st, cache, n.self), Auth: n.auth, Profiles: st, Catalogue: st, Libraries: st, Tasks: n.scheduler, Jobs: st, Backups: n.restores, Maintenance: st, NowPlaying: cache, ProfileAdmin: st, Avatars: st, Providers: n.providers, ProviderSettings: st, Plugins: p.plugins, Collections: st, Preferences: st, Playlists: st, People: st, PersonDescriber: n.providers, Editing: st, History: st, Pictures: st, Themes: st, Watching: st, Playing: p.files, Subtitles: p.fetcher, Playbacks: n.sessions, Owners: p.owners, Placer: p.placer, NodeKey: p.nodeKey, HLS: n.remuxer, Signer: p.signer, Artwork: n.pictures, Previews: st, PreviewFiles: n.previews, Downloads: st, Conversions: n.conversions, Limits: cache, Activity: st, Events: n.hub, Audience: st, Webhooks: st, Importer: n.imports, HistoryImports: st, TrustedProxies: p.trusted, Network: st, Storage: st, Stores: n.stores, Nodes: st, Secure: n.secured, Jellyfin: n.jellyfin, Setup: setup, Postgres: st, Valkey: cache, Web: web,
			Metrics: metrics(version, n.self, n.remuxer, n.sessions, p.placer, p.sent, n.finished, cluster{lead: n.scheduler, st: st, nodes: cache, self: p.placer}),
			Sent:    p.sent,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(n.logger.Handler(), slog.LevelWarn),
	}, nil
}

// workers are the node's job workers. Each kind of job has slots of its own, so a big import's
// file reading never holds up its matching and a scan never waits behind either.
func (n *node) workers() []*jobs.Worker {
	st, logger, hub := n.st, n.logger, n.hub
	scanner := jobs.NewWorker(st, logger, n.id, scanSlots, map[domain.JobKind]jobs.Handler{
		domain.JobScanLibrary: scanLibrary(st, scan.New(st, n.tools, logger), hub, logger),
	}, hub, nil, n.finished)
	matching := map[domain.JobKind]jobs.Handler{
		domain.JobIdentify: identify.Handler(st, n.providers, n.pictures, domain.LocaleOf(n.lang), hub.Raise, logger),
	}
	// A node without yt-dlp leaves themes to one with it.
	if n.tools.YTDLP.Path != "" {
		matching[domain.JobTheme] = themerr.Fetch(st, n.pictures, n.cache, themerr.DB, n.tools.YTDLP.Path, n.tools.FFmpeg.Path, logger)
	}
	matcher := jobs.NewWorker(st, logger, n.id, identifySlots, matching, hub, nil, n.finished)
	notifier := jobs.NewWorker(st, logger, n.id, webhookSlots, map[domain.JobKind]jobs.Handler{
		domain.JobDeliverWebhook: webhook.Deliver(st),
	}, hub, nil, n.finished)
	importer := jobs.NewWorker(st, logger, n.id, importSlots, map[domain.JobKind]jobs.Handler{
		domain.JobImportHistory: n.imports.Run,
	}, hub, nil, n.finished)
	// Each kind of job that reads media has a worker of its own, so however many of one are queued,
	// the others keep their slot, and each gives way to playback and keeps to its timing's hours.
	reader := func(kind domain.JobKind, h jobs.Handler) *jobs.Worker {
		return jobs.NewWorker(st, logger, n.id, mediaSlots, map[domain.JobKind]jobs.Handler{kind: h}, hub, n.gate, n.finished)
	}
	// Conversions have slots of their own, so a long one never holds up a scan, and each holds a
	// transcode slot its node's playbacks may take, so they never starve them. A conversion is asked
	// for by someone waiting on it, as a playback is, so it waits for no playback: on a server played
	// every evening, one held back by each would not be ready for days. A node that never encodes
	// leaves them to one that does.
	converter := jobs.NewWorker(st, logger, n.id, playback.MaxConversions, map[domain.JobKind]jobs.Handler{
		domain.JobConvert: n.conversions.Convert,
	}, hub, jobs.When(n.self.TakesTranscodes), n.finished)
	return []*jobs.Worker{
		scanner, matcher, notifier, importer,
		reader(domain.JobKeyframes, analysis.Keyframes(st)),
		reader(domain.JobKeyframeWalk, analysis.WalkKeyframes(st, n.tools)),
		reader(domain.JobMarkers, analysis.Markers(st, n.tools.Fingerprint, n.tools.Shades)),
		reader(domain.JobPreviews, analysis.MakePreviews(st, n.tools, n.previews, logger)),
		reader(domain.JobProbe, analysis.Probe(st, n.tools)),
		converter,
	}
}

// serve runs the node's work and serves until ctx ends; stopped for a restore, it answers the
// restore.
func (n *node) serve(ctx context.Context) error {
	logger := n.logger
	// What keeps streams playing (serving, telling the others where this node is, ending what has
	// stopped) outlives the signal, as the node drains; what begins new work stops at it.
	background, stopBackground := context.WithCancel(context.WithoutCancel(ctx))
	var wg sync.WaitGroup
	defer func() {
		stopBackground()
		wg.Wait()
	}()
	// On the signal's context, not background's, so its streams end before Shutdown waits on them.
	wg.Go(func() { n.hub.Run(ctx) })
	wg.Go(func() { n.scheduler.Run(ctx) })
	wg.Go(func() { n.gate.Run(background) })
	for _, w := range n.workers() {
		wg.Go(func() { w.Run(ctx) })
	}
	wg.Go(func() { sweepPlaybacks(background, n.sessions, n.remuxer, logger) })
	wg.Go(func() { pruneConversions(background, n.conversions, logger) })
	wg.Go(func() { n.self.Run(background, n.hub.Subscribe) })
	if n.address != "" {
		wg.Go(func() { advertise(background, n.cache, n.self.Node, n.remuxer.Changes(), n.self.Changes(), logger) })
	}
	wg.Go(func() {
		if err := watch.New(n.st, logger).Run(ctx); err != nil {
			logger.WarnContext(ctx, "libraries are scanned on schedule only", slog.Any("err", err))
		}
	})
	switch n.discovery {
	case domain.DiscoveryBroadcast:
		wg.Go(func() { answerDiscovery(background, n.srv.Addr, n.secured.Scheme, n.info, logger) })
	case domain.DiscoveryOff:
	}
	wg.Go(func() { n.secured.Run(background) })
	wg.Go(func() {
		n.stores.Run(background, storage.Cluster{Node: n.id, Subscribe: n.hub.Subscribe, Raise: n.hub.Raise, Nodes: nodeIDs(n.cache)})
	})
	wg.Go(func() { n.jellyfin.Run(background) })
	logger.InfoContext(ctx, "serving", slog.String("addr", n.srv.Addr), slog.String("version", n.info.Version))
	err := listenUntilDone(ctx, n.srv, n.secured.Listen, func() {
		if errors.As(context.Cause(ctx), new(*stoppedForRestore)) {
			endStreams(n.self, n.remuxer)
			return
		}
		again := make(chan os.Signal, 1)
		signal.Notify(again, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(again)
		drain(context.WithoutCancel(ctx), n.self, n.remuxer, again, drainFor, logger)
	})
	if stopped, ok := errors.AsType[*stoppedForRestore](context.Cause(ctx)); ok && err == nil {
		return stopped
	}
	return err
}

// discoveryMode is how clients find the server, PHOTON_DISCOVERY: by broadcast unless it is off.
func discoveryMode() (domain.Discovery, error) {
	mode, err := domain.Parse("discovery", cmp.Or(os.Getenv("PHOTON_DISCOVERY"), string(domain.DiscoveryBroadcast)), domain.Discoveries())
	if err != nil {
		return "", fmt.Errorf("PHOTON_DISCOVERY: %w", err)
	}
	return mode, nil
}
