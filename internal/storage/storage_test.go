//go:build integration

package storage

import (
	"context"
	"errors"
	"image"
	_ "image/png"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

func migrated(t *testing.T) *store.Store {
	t.Helper()
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), db, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

// testBucket is a fresh folder of the bucket TEST_S3_ENDPOINT and TEST_S3_BUCKET name.
func testBucket(t *testing.T) domain.Bucket {
	t.Helper()
	b := domain.Bucket{
		Endpoint: os.Getenv("TEST_S3_ENDPOINT"), Name: os.Getenv("TEST_S3_BUCKET"), Folder: uuid.NewV7().String(),
		AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"), SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		Delivery: domain.DeliverProxy,
	}
	if b.Endpoint == "" || b.Name == "" {
		t.Fatal("TEST_S3_ENDPOINT and TEST_S3_BUCKET are unset")
	}
	return b
}

// hub tells every node's subscription of each event raised, as Valkey does.
type hub struct {
	mu   sync.Mutex
	subs []chan domain.Event
}

func (h *hub) Subscribe() (<-chan domain.Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := make(chan domain.Event, 16)
	h.subs = append(h.subs, c)
	return c, func() {}
}

func (h *hub) Raise(_ context.Context, e domain.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.subs {
		c <- e
	}
}

// node is a server node of a cluster sharing settings and events.
type node struct {
	id     uuid.UUID
	stores *Stores
}

// cluster starts n nodes, each with a cache folder of its own, running until the test ends, in a
// cluster also up are others.
func cluster(t *testing.T, st *store.Store, n int, others ...uuid.UUID) (*hub, []node) {
	t.Helper()
	h := &hub{}
	nodes := make([]node, n)
	ids := make([]uuid.UUID, n, n+len(others))
	for i := range nodes {
		stores, err := Open(t.Context(), st, t.TempDir(), slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = stores.Close() })
		nodes[i] = node{id: uuid.NewV7(), stores: stores}
		ids[i] = nodes[i].id
	}
	ids = append(ids, others...)
	ctx, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() { stop(); wg.Wait() })
	for _, n := range nodes {
		c := Cluster{
			Node: n.id, Subscribe: h.Subscribe, Raise: h.Raise,
			Nodes: func(context.Context) ([]uuid.UUID, error) { return ids, nil },
		}
		wg.Go(func() { n.stores.Run(ctx, c) })
	}
	return h, nodes
}

// waitFor waits up to a minute for done to hold.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	limit := time.After(time.Minute)
	for !done() {
		select {
		case <-tick.C:
		case <-limit:
			t.Fatalf("waited a minute for %s", what)
		}
	}
}

func kept(t *testing.T, p *Place, key string) bool {
	t.Helper()
	ok, err := p.Exists(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// Two nodes keeping artwork on their own disks move it to a bucket while in use: each copies its
// own disk, what is put during the move is kept in both, and both keep everything in the bucket
// once both copies are done.
func TestEachNodesDiskIsMovedToTheBucketWhileInUse(t *testing.T) {
	st := migrated(t)
	ctx := t.Context()
	h, nodes := cluster(t, st, 2)
	for i, n := range nodes {
		if err := n.stores.Artwork.Put(ctx, "poster-"+string(rune('a'+i)), strings.NewReader("poster")); err != nil {
			t.Fatal(err)
		}
		if err := n.stores.Previews.Put(ctx, "part/trickplay/0.jpg", strings.NewReader("sheet")); err != nil {
			t.Fatal(err)
		}
	}
	to := domain.Storage{Kind: domain.StorageBucket, Bucket: testBucket(t)}
	if err := st.StartStorageMove(ctx, to); err != nil {
		t.Fatal(err)
	}
	h.Raise(ctx, domain.Event{Kind: domain.EventStorageChanged})
	waitFor(t, "the move to be done", func() bool {
		now, err := st.Storage(ctx)
		return err == nil && now == to
	})
	for _, n := range nodes {
		waitFor(t, "every node to keep things in the bucket", func() bool { return kept(t, &n.stores.Artwork, "poster-b") })
		for _, key := range []string{"poster-a", "poster-b"} {
			if !kept(t, &n.stores.Artwork, key) {
				t.Errorf("node %s: %s was left behind", n.id, key)
			}
		}
		if !kept(t, &n.stores.Previews, "part/trickplay/0.jpg") {
			t.Errorf("node %s: the sheet was left behind", n.id)
		}
	}
	if m, moving, err := st.StorageMove(ctx); moving || err != nil {
		t.Errorf("a move %+v (%v) is still under way", m, err)
	}
}

// A node writes to both places while a move is under way, so nothing put after its copy began is
// left behind; and a move cancelled leaves everything where it was.
func TestWhatIsPutDuringAMoveIsKeptInBothAndACancelledMoveLeavesThingsBe(t *testing.T) {
	st := migrated(t)
	ctx := t.Context()
	// Another node, whose copy is never done, holds the move open.
	other := uuid.NewV7()
	h, nodes := cluster(t, st, 1, other)
	n := nodes[0]
	to := domain.Storage{Kind: domain.StorageBucket, Bucket: testBucket(t)}
	if err := st.StartStorageMove(ctx, to); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimMoveSource(ctx, other, time.Now()); err != nil {
		t.Fatal(err)
	}
	h.Raise(ctx, domain.Event{Kind: domain.EventStorageChanged})
	waitFor(t, "the node to write to both places", func() bool {
		n.stores.mu.Lock()
		defer n.stores.mu.Unlock()
		return n.stores.to != nil
	})
	if err := n.stores.Artwork.Put(ctx, "during", strings.NewReader("poster")); err != nil {
		t.Fatal(err)
	}
	n.stores.mu.Lock()
	bucket := n.stores.to.artwork
	n.stores.mu.Unlock()
	if ok, err := bucket.Exists(ctx, "during"); !ok || err != nil {
		t.Errorf("a poster put during the move is not in the bucket: %v, %v", ok, err)
	}
	if err := st.CancelStorageMove(ctx); err != nil {
		t.Fatal(err)
	}
	h.Raise(ctx, domain.Event{Kind: domain.EventStorageChanged})
	waitFor(t, "the node to write to one place", func() bool {
		n.stores.mu.Lock()
		defer n.stores.mu.Unlock()
		return n.stores.to == nil
	})
	if now, _ := st.Storage(ctx); now.Kind != domain.StorageDisk {
		t.Errorf("after the move was cancelled things are kept %+v, want on disk", now)
	}
	if err := n.stores.Artwork.Put(ctx, "after", strings.NewReader("poster")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := bucket.Exists(ctx, "after"); ok {
		t.Error("a poster put after the move was cancelled was put in the bucket")
	}
	if _, err := n.stores.Artwork.Open(ctx, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("opening what is not kept: %v", err)
	}
}

// From one bucket every node shares to another, one node copies, once every node writes to both.
func TestOneNodeCopiesFromABucketToAnother(t *testing.T) {
	settle := settleFor
	settleFor = time.Second
	t.Cleanup(func() { settleFor = settle })
	st := migrated(t)
	ctx := t.Context()
	from := domain.Storage{Kind: domain.StorageBucket, Bucket: testBucket(t)}
	if err := st.SetStorage(ctx, from); err != nil {
		t.Fatal(err)
	}
	h, nodes := cluster(t, st, 3)
	if err := nodes[0].stores.Artwork.Put(ctx, "poster", strings.NewReader("poster")); err != nil {
		t.Fatal(err)
	}
	to := domain.Storage{Kind: domain.StorageBucket, Bucket: testBucket(t)}
	if err := st.StartStorageMove(ctx, to); err != nil {
		t.Fatal(err)
	}
	h.Raise(ctx, domain.Event{Kind: domain.EventStorageChanged})
	waitFor(t, "the move to be done", func() bool {
		now, err := st.Storage(ctx)
		return err == nil && now == to
	})
	for _, n := range nodes {
		waitFor(t, "every node to keep things in the new bucket", func() bool { return kept(t, &n.stores.Artwork, "poster") })
	}
}

func TestABucketIsCheckedBeforeItIsChosen(t *testing.T) {
	stores := &Stores{}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := stores.Check(ctx, testBucket(t)); err != nil {
		t.Errorf("a bucket the server can use: %v", err)
	}
	wrong := testBucket(t)
	wrong.SecretKey = "wrong"
	if err := stores.Check(ctx, wrong); err == nil {
		t.Error("a bucket the server cannot sign for passed its check")
	}
}

// Where clients are sent to the bucket, its origin is told to the page's policy, and a picture
// there given for the browser to try.
func TestClientsSentToTheBucketAreGivenAPictureToTry(t *testing.T) {
	st := migrated(t)
	b := testBucket(t)
	b.Delivery = domain.DeliverRedirect
	if err := st.SetStorage(t.Context(), domain.Storage{Kind: domain.StorageBucket, Bucket: b}); err != nil {
		t.Fatal(err)
	}
	stores, err := Open(t.Context(), st, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if stores.Origin() != b.Endpoint {
		t.Errorf("origin %q, want the bucket's own %q", stores.Origin(), b.Endpoint)
	}
	link, err := stores.Probe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cfg, format, err := image.DecodeConfig(resp.Body)
	if err != nil || format != "png" || cfg.Width != 1 {
		t.Errorf("the probe read as %s %dx%d (%v), want a one-pixel PNG", format, cfg.Width, cfg.Height, err)
	}
}
