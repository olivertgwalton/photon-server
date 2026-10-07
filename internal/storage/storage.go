// Package storage keeps artwork and previews where an admin has chosen, and moves every node there
// when the choice changes.
package storage

import (
	"bytes"
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
}

// Stores are this node's artwork and previews, kept where the settings say.
type Stores struct {
	Artwork, Previews Place
	settings          settings
	// cache is this node's own cache folder.
	cache string
	log   *slog.Logger

	mu sync.Mutex
	// applied is what the stores were opened from, and closing the folders they hold.
	applied domain.Storage
	closing func() error
}

// Open opens the stores where the settings say, refusing to start a node that cannot reach them.
func Open(ctx context.Context, s settings, cache string, log *slog.Logger) (*Stores, error) {
	st, err := s.Storage(ctx)
	if err != nil {
		return nil, err
	}
	stores := &Stores{settings: s, cache: cache, log: log}
	if err := stores.apply(ctx, st); err != nil {
		return nil, fmt.Errorf("artwork and previews: %w", err)
	}
	return stores, nil
}

func (s *Stores) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing()
}

// Run keeps the stores where the settings say until ctx ends, reading them again as an admin
// changes them and every rereadEvery.
func (s *Stores) Run(ctx context.Context, subscribe func() (<-chan domain.Event, func())) {
	follow.Events(ctx, subscribe, rereadEvery, s.reread, domain.EventStorageChanged)
}

// reread moves the stores where the settings now say. What cannot be reached leaves them where
// they are, to be tried again.
func (s *Stores) reread(ctx context.Context) {
	st, err := s.settings.Storage(ctx)
	if err == nil {
		err = s.apply(ctx, st)
	}
	if err != nil && ctx.Err() == nil {
		s.log.WarnContext(ctx, "artwork and previews not moved", slog.Any("err", err))
	}
}

func (s *Stores) apply(ctx context.Context, st domain.Storage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing != nil && st == s.applied {
		return nil
	}
	artwork, previews, closing, err := open(ctx, st, s.cache)
	if err != nil {
		return err
	}
	s.Artwork.at.Store(&held{artwork})
	s.Previews.at.Store(&held{previews})
	if s.closing != nil {
		// What is being read from the folders left stays open until it is closed.
		if err := s.closing(); err != nil {
			s.log.WarnContext(ctx, "artwork and previews' old folders not closed", slog.Any("err", err))
		}
	}
	s.applied, s.closing = st, closing
	return nil
}

// Empty reports whether this node keeps no artwork and no previews where it keeps them now: on
// disk, its own; in a bucket, every node's.
func (s *Stores) Empty(ctx context.Context) (bool, error) {
	for _, p := range []*Place{&s.Artwork, &s.Previews} {
		for _, err := range p.List(ctx, "") {
			return false, err
		}
	}
	return true, nil
}

// open opens the artwork and previews st says, and answers closing what they hold open.
func open(ctx context.Context, st domain.Storage, cache string) (artwork, previews objects, closing func() error, err error) {
	switch st.Kind {
	case domain.StorageDisk:
		a, err := blob.OpenDir(filepath.Join(cache, "artwork"))
		if err != nil {
			return nil, nil, nil, err
		}
		p, err := blob.OpenDir(filepath.Join(cache, "previews"))
		if err != nil {
			return nil, nil, nil, errors.Join(err, a.Close())
		}
		return a, p, func() error { return errors.Join(a.Close(), p.Close()) }, nil
	case domain.StorageBucket:
		b, err := blob.OpenBucket(ctx, config(st.Bucket))
		if err != nil {
			return nil, nil, nil, err
		}
		return b.Within("artwork"), b.Within("previews"), func() error { return nil }, nil
	}
	return nil, nil, nil, fmt.Errorf("storage %q is neither disk nor bucket", st.Kind)
}

func config(b domain.Bucket) blob.Config {
	return blob.Config{
		Endpoint: b.Endpoint, Bucket: b.Name, Folder: b.Folder, Region: b.Region,
		AccessKey: b.AccessKey, SecretKey: b.SecretKey,
	}
}

// checkKey is the object Check writes, outside the folders artwork and previews are kept in.
const checkKey = ".photon-check"

// Check reports why b cannot keep artwork and previews: it cannot be reached, or what is put there
// cannot be read back, listed and removed.
func (*Stores) Check(ctx context.Context, b domain.Bucket) error {
	bucket, err := blob.OpenBucket(ctx, config(b))
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
	_ = o.Close()
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
