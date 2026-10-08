package backup

import (
	"context"
	"errors"
	"path/filepath"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

// RestoreKept is how long a restore under way lasts after its node last kept it: a node that dies
// restoring leaves the database as it was, its one transaction undone, and the others start again
// once it lapses.
const RestoreKept = 30 * time.Second

var ErrRestoring = errors.New("a restore is already under way")

// Restores asks every node to stop so that this one may restore one of its dumps, as Jellyfin
// schedules a restore and restarts.
type Restores struct {
	Restorer Restorer
	Dir      string
	Node     uuid.UUID
	KV       *kv.KV
	Raise    func(context.Context, domain.Event)
}

// Begin asks for the dump named name, in this node's folder, to be restored, refusing one this
// node does not keep (fs.ErrNotExist), one newer than this binary (ErrNewer), and a second restore
// while one is under way (ErrRestoring).
func (s Restores) Begin(ctx context.Context, name string) error {
	f, err := Open(s.Dir, name)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if _, _, err := s.Restorer.check(ctx, filepath.Join(s.Dir, name)); err != nil {
		return err
	}
	r := domain.Restore{Dump: name, Node: s.Node, Started: time.Now().UTC(), Phase: domain.RestoreStopping}
	ok, err := s.KV.BeginRestore(ctx, r, RestoreKept)
	if err != nil {
		return err
	}
	if !ok {
		return ErrRestoring
	}
	s.Raise(ctx, domain.Event{Kind: domain.EventRestoreStarted, Details: map[string]any{"dump": name}})
	return nil
}

func (s Restores) Underway(ctx context.Context) (domain.Restore, bool, error) {
	return s.KV.Restoring(ctx)
}

func (s Restores) Last(ctx context.Context) (domain.RestoreOutcome, bool, error) {
	return s.KV.LastRestore(ctx)
}
