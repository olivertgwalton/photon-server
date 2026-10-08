// Package storage keeps artwork and previews where an admin has chosen, and moves every node there
// when the choice changes.
package storage

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/follow"
)

const (
	// rereadEvery is how often a node reads where things are kept, besides as an admin changes it:
	// a change whose event was lost with Valkey's connection is taken up within it.
	rereadEvery = time.Minute
)

// objects is what a folder of objects and a bucket both do.
type objects interface {
	Open(ctx context.Context, key string) (blob.Object, error)
	Exists(ctx context.Context, key string) (bool, error)
	Put(ctx context.Context, key string, r io.Reader) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) iter.Seq2[blob.Entry, error]
}

// Place is objects kept wherever is chosen now, which a change of choice moves while in use.
type Place struct {
	at atomic.Pointer[held]
}

type held struct{ objects }

func (p *Place) Open(ctx context.Context, key string) (blob.Object, error) {
	return p.at.Load().Open(ctx, key)
}

func (p *Place) Exists(ctx context.Context, key string) (bool, error) {
	return p.at.Load().Exists(ctx, key)
}

func (p *Place) Put(ctx context.Context, key string, r io.Reader) error {
	return p.at.Load().Put(ctx, key, r)
}

func (p *Place) Delete(ctx context.Context, key string) error {
	return p.at.Load().Delete(ctx, key)
}

func (p *Place) List(ctx context.Context, prefix string) iter.Seq2[blob.Entry, error] {
	return p.at.Load().List(ctx, prefix)
}

type settings interface {
	Storage(ctx context.Context) (domain.Storage, error)
	StorageMove(ctx context.Context) (domain.StorageMove, bool, error)
	ClaimMoveSource(ctx context.Context, source uuid.UUID, stale time.Time) (bool, error)
	ReportMoveSource(ctx context.Context, source uuid.UUID, copied, total int, done bool) error
	FinishStorageMove(ctx context.Context, sources []uuid.UUID) (bool, error)
}

// Stores are this node's artwork and previews, kept where the settings say.
type Stores struct {
	Artwork, Previews Place
	settings          settings
	// cache is this node's own cache folder.
	cache string
	log   *slog.Logger

	mu sync.Mutex
	// at is where things are kept, and to where they are being moved to, if anywhere.
	at, to *opened
	// copying stops the copy this node is making, if it is making one.
	copying context.CancelFunc
	copies  sync.WaitGroup
}

// opened is one place's artwork and previews, open.
type opened struct {
	st                domain.Storage
	artwork, previews objects
	// root is the bucket they are kept in, nil on disk; origin is where clients are sent to read
	// them, or "" where they are sent nowhere.
	root    *blob.Bucket
	origin  string
	closing func() error
}

// Open opens the stores where the settings say, refusing to start a node that cannot reach them.
func Open(ctx context.Context, s settings, cache string, log *slog.Logger) (*Stores, error) {
	st, err := s.Storage(ctx)
	if err != nil {
		return nil, err
	}
	stores := &Stores{settings: s, cache: cache, log: log}
	if err := stores.apply(ctx, st, nil); err != nil {
		return nil, fmt.Errorf("artwork and previews: %w", err)
	}
	return stores, nil
}

func (s *Stores) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.at.closing()
	if s.to != nil {
		err = errors.Join(err, s.to.closing())
	}
	return err
}

// Run keeps the stores where the settings say, and moves what is kept as an admin asks, until ctx
// ends, reading the settings again as they change and every rereadEvery.
func (s *Stores) Run(ctx context.Context, c Cluster) {
	follow.Events(ctx, c.Subscribe, rereadEvery, func(ctx context.Context) { s.reread(ctx, c) }, domain.EventStorageChanged)
	s.copies.Wait()
}

// reread keeps the stores where the settings now say, writing to both places of a move under way
// and taking this node's part in it. What cannot be reached leaves them as they are, to be tried
// again.
func (s *Stores) reread(ctx context.Context, c Cluster) {
	st, err := s.settings.Storage(ctx)
	var m domain.StorageMove
	moving := false
	if err == nil {
		m, moving, err = s.settings.StorageMove(ctx)
	}
	var to *domain.Storage
	if moving {
		to = &m.To
	}
	if err == nil {
		err = s.apply(ctx, st, to)
	}
	if err == nil && moving {
		err = s.move(ctx, c, st, m)
	}
	if err != nil && ctx.Err() == nil {
		s.log.WarnContext(ctx, "artwork and previews not moved", slog.Any("err", err))
	}
}

// apply keeps the stores at st, and, where to is a place they are being moved to, writes to it
// too. A place unchanged is kept open.
func (s *Stores) apply(ctx context.Context, st domain.Storage, to *domain.Storage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, err := s.reopen(ctx, s.at, &st)
	if err != nil {
		return err
	}
	next, err := s.reopen(ctx, s.to, to)
	if err != nil {
		if at != s.at {
			err = errors.Join(err, at.closing())
		}
		return err
	}
	artwork, previews := at.artwork, at.previews
	if next != nil {
		artwork, previews = both{from: at.artwork, to: next.artwork}, both{from: at.previews, to: next.previews}
	} else if s.copying != nil {
		// The move is over, finished or cancelled.
		s.copying()
		s.copying = nil
	}
	s.Artwork.at.Store(&held{artwork})
	s.Previews.at.Store(&held{previews})
	// What is being read from places left stays open until it is closed.
	for _, left := range []*opened{s.at, s.to} {
		if left != nil && left != at && left != next {
			if err := left.closing(); err != nil {
				s.log.WarnContext(ctx, "artwork and previews' old folders not closed", slog.Any("err", err))
			}
		}
	}
	s.at, s.to = at, next
	return nil
}

// reopen answers was where st is the place it is open at, or st opened; nil for no st.
func (s *Stores) reopen(ctx context.Context, was *opened, st *domain.Storage) (*opened, error) {
	switch {
	case st == nil:
		return nil, nil
	case was != nil && was.st == *st:
		return was, nil
	case s.at != nil && s.at.st == *st:
		// The place moved to is kept at now: the move is finished.
		return s.at, nil
	case s.to != nil && s.to.st == *st:
		return s.to, nil
	}
	return open(ctx, *st, s.cache)
}

// Origin is where clients are sent to read artwork and previews, or "" where they are sent nowhere.
func (s *Stores) Origin() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.at.origin
}

// probeKey is a picture a browser is sent to, to find whether it reaches the bucket.
const probeKey = ".photon-probe.png"

// probePNG is one transparent pixel.
var probePNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89" +
	"\x00\x00\x00\rIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82")

// Probe answers a link to a picture in the bucket clients are sent to, for a browser to find
// whether it can read from there, or "" where clients are sent nowhere.
func (s *Stores) Probe(ctx context.Context) (string, error) {
	s.mu.Lock()
	root := s.at.root
	s.mu.Unlock()
	if root == nil {
		return "", nil
	}
	if ok, err := root.Exists(ctx, probeKey); err != nil || !ok {
		if err := root.Put(ctx, probeKey, bytes.NewReader(probePNG)); err != nil {
			return "", err
		}
	}
	return root.Link(ctx, probeKey)
}

// open opens the artwork and previews st says.
func open(ctx context.Context, st domain.Storage, cache string) (*opened, error) {
	switch st.Kind {
	case domain.StorageDisk:
		a, err := blob.OpenDir(filepath.Join(cache, "artwork"))
		if err != nil {
			return nil, err
		}
		p, err := blob.OpenDir(filepath.Join(cache, "previews"))
		if err != nil {
			return nil, errors.Join(err, a.Close())
		}
		return &opened{st: st, artwork: a, previews: p, closing: func() error { return errors.Join(a.Close(), p.Close()) }}, nil
	case domain.StorageBucket:
		c, err := config(st.Bucket)
		if err != nil {
			return nil, err
		}
		b, err := blob.OpenBucket(ctx, c)
		if err != nil {
			return nil, err
		}
		origin, err := b.Origin(ctx)
		if err != nil {
			return nil, err
		}
		return &opened{
			st: st, artwork: b.Within("artwork"), previews: b.Within("previews"), root: b, origin: origin,
			closing: func() error { return nil },
		}, nil
	}
	return nil, fmt.Errorf("storage %q is neither disk nor bucket", st.Kind)
}

// awsEndpoint is Amazon S3's, where a bucket is that names no store.
const awsEndpoint = "https://s3.amazonaws.com"

func config(b domain.Bucket) (blob.Config, error) {
	c := blob.Config{
		Endpoint: b.Endpoint, Bucket: b.Name, Folder: b.Folder, Region: b.Region,
		AccessKey: b.AccessKey, SecretKey: b.SecretKey,
	}
	switch b.Delivery {
	case domain.DeliverProxy:
	case domain.DeliverRedirect:
		c.LinkEndpoint = cmp.Or(b.PublicEndpoint, b.Endpoint, awsEndpoint)
	default:
		return blob.Config{}, fmt.Errorf("delivery %q is neither %q nor %q", b.Delivery, domain.DeliverProxy, domain.DeliverRedirect)
	}
	return c, nil
}

// checkKey is the object Check writes, outside the folders artwork and previews are kept in.
const checkKey = ".photon-check"

// Check reports why b cannot keep artwork and previews: it cannot be reached, or what is put there
// cannot be read back, listed and removed.
func (*Stores) Check(ctx context.Context, b domain.Bucket) error {
	c, err := config(b)
	if err != nil {
		return err
	}
	bucket, err := blob.OpenBucket(ctx, c)
	if err != nil {
		return err
	}
	want := []byte("photon")
	if err := bucket.Put(ctx, checkKey, bytes.NewReader(want)); err != nil {
		return fmt.Errorf("nothing can be put in the bucket: %w", err)
	}
	o, err := bucket.Open(ctx, checkKey)
	if err != nil {
		return fmt.Errorf("what was put in the bucket cannot be read: %w", err)
	}
	got, err := io.ReadAll(o)
	err = errors.Join(err, o.Close())
	if err != nil || !bytes.Equal(got, want) {
		return fmt.Errorf("what was put in the bucket did not read back as it was: %w", err)
	}
	listed := false
	for e, err := range bucket.List(ctx, checkKey) {
		if err != nil {
			return fmt.Errorf("the bucket cannot be listed: %w", err)
		}
		listed = listed || e.Key == checkKey
	}
	if !listed {
		return errors.New("what was put in the bucket is not listed")
	}
	if err := bucket.Delete(ctx, checkKey); err != nil {
		return fmt.Errorf("nothing can be removed from the bucket: %w", err)
	}
	return nil
}
