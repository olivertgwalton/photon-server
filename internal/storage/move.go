package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"iter"
	"log/slog"
	"slices"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// settleFor is how long a copy from the bucket every node shares waits, from the move's start,
// before it lists what to copy: by then every node has heard of the move and writes to both
// places, so nothing put after the listing is left behind. A variable for the test to shorten.
var settleFor = rereadEvery + 30*time.Second

const (
	// staleAfter is how long a copy from the shared bucket may say nothing before another node
	// takes it over, as one whose node stopped says nothing.
	staleAfter = 2 * time.Minute
	// reportEvery is how often a copy says how far it has got.
	reportEvery = 5 * time.Second
)

// Cluster is what a node needs of the others to keep what is kept where an admin says.
type Cluster struct {
	Node      uuid.UUID
	Subscribe func() (<-chan domain.Event, func())
	Raise     func(ctx context.Context, e domain.Event)
	// Nodes answers the other nodes that are up, whose disks a move waits for.
	Nodes func(ctx context.Context) ([]uuid.UUID, error)
}

// move takes this node's part in moving what is kept from st as m says: it copies its own disk,
// or what it moves to its own disk, or, with no disk at either end, the shared bucket if no other
// node is; and, once its part is done, keeps everything where it is moved to if every part is.
func (s *Stores) move(ctx context.Context, c Cluster, st domain.Storage, m domain.StorageMove) error {
	source, stale := c.Node, time.Now().Add(time.Hour) // a node's own part is always its own
	if domain.Shared(st, m.To) {
		source, stale = uuid.UUID{}, time.Now().Add(-staleAfter)
	}
	if i := slices.IndexFunc(m.Sources, func(src domain.MoveSource) bool { return src.Node == source }); i >= 0 && m.Sources[i].Done {
		return s.finish(ctx, c, st, m)
	}
	s.mu.Lock()
	busy := s.copying != nil
	s.mu.Unlock()
	if busy {
		return nil
	}
	claimed, err := s.settings.ClaimMoveSource(ctx, source, stale)
	if err != nil || !claimed {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.to == nil || s.copying != nil {
		return nil
	}
	from, to := s.at, s.to
	copying, stop := context.WithCancel(ctx)
	s.copying = stop
	s.copies.Go(func() {
		defer stop()
		if source == (uuid.UUID{}) {
			select {
			case <-copying.Done():
				return
			case <-time.After(time.Until(m.Started.Add(settleFor))):
			}
		}
		err := s.copy(copying, source, from, to)
		if err == nil {
			err = s.finish(copying, c, st, m)
		}
		if err != nil && copying.Err() == nil {
			s.log.WarnContext(copying, "artwork and previews not moved", slog.Any("err", err))
		}
		s.mu.Lock()
		s.copying = nil
		s.mu.Unlock()
	})
	return nil
}

// copy copies what is kept at from that to lacks, saying how far it has got as it goes.
func (s *Stores) copy(ctx context.Context, source uuid.UUID, from, to *opened) error {
	type pair struct{ from, to objects }
	pairs := []pair{{from.artwork, to.artwork}, {from.previews, to.previews}}
	var missing [][]string
	total, copied := 0, 0
	for _, p := range pairs {
		there := map[string]bool{}
		for e, err := range p.to.List(ctx, "") {
			if err != nil {
				return err
			}
			there[e.Key] = true
		}
		var lacking []string
		for e, err := range p.from.List(ctx, "") {
			if err != nil {
				return err
			}
			total++
			if there[e.Key] {
				copied++
			} else {
				lacking = append(lacking, e.Key)
			}
		}
		missing = append(missing, lacking)
	}
	said := time.Now()
	for i, p := range pairs {
		for _, key := range missing[i] {
			if err := copyObject(ctx, p.from, p.to, key); err != nil {
				return err
			}
			copied++
			if time.Since(said) >= reportEvery {
				if err := s.settings.ReportMoveSource(ctx, source, copied, total, false); err != nil {
					return err
				}
				said = time.Now()
			}
		}
	}
	return s.settings.ReportMoveSource(ctx, source, copied, total, true)
}

func copyObject(ctx context.Context, from, to objects, key string) error {
	o, err := from.Open(ctx, key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // removed since it was listed, as a swept picture is
	}
	if err != nil {
		return err
	}
	defer o.Close()
	return to.Put(ctx, key, o)
}

// finish keeps everything where m moves it, once every part of the move is done, and tells every
// node.
func (s *Stores) finish(ctx context.Context, c Cluster, st domain.Storage, m domain.StorageMove) error {
	sources := []uuid.UUID{{}}
	if !domain.Shared(st, m.To) {
		others, err := c.Nodes(ctx)
		if err != nil {
			return err
		}
		sources = append(slices.DeleteFunc(others, func(n uuid.UUID) bool { return n == c.Node }), c.Node)
	}
	finished, err := s.settings.FinishStorageMove(ctx, sources)
	if err != nil || !finished {
		return err
	}
	c.Raise(ctx, domain.Event{Kind: domain.EventStorageChanged})
	return nil
}

// both is objects kept in one place while they are moved to another: read from the first, and
// written to both.
type both struct{ from, to objects }

func (b both) Open(ctx context.Context, key string) (blob.Object, error) {
	return b.from.Open(ctx, key)
}

func (b both) Exists(ctx context.Context, key string) (bool, error) { return b.from.Exists(ctx, key) }

func (b both) List(ctx context.Context, prefix string) iter.Seq2[blob.Entry, error] {
	return b.from.List(ctx, prefix)
}

// Put keeps r's bytes in both places; failing in either, it fails, as a write the move could lose.
func (b both) Put(ctx context.Context, key string, r io.Reader) error {
	// ponytail: holds the object in memory to write it twice; they are pictures and sheets of a
	// few MiB, a theme tune a little more.
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return errors.Join(b.from.Put(ctx, key, bytes.NewReader(data)), b.to.Put(ctx, key, bytes.NewReader(data)))
}

func (b both) Delete(ctx context.Context, key string) error {
	return errors.Join(b.from.Delete(ctx, key), b.to.Delete(ctx, key))
}
