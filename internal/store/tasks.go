package store

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"gorm.io/gen/field"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// HoldLease takes or renews the named lease for node, for ttl. It answers false while another
// node holds an unexpired lease. One statement does it, so two nodes asking at once cannot both
// win; gen has no conditional upsert, so it is SQL.
func (s *Store) HoldLease(ctx context.Context, name string, node uuid.UUID, ttl time.Duration) (bool, error) {
	var holder string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO leader (name, node_id, expires_at) VALUES ($1, $2, now() + $3)
		ON CONFLICT (name) DO UPDATE SET node_id = excluded.node_id, expires_at = excluded.expires_at
		WHERE leader.node_id = excluded.node_id OR leader.expires_at < now()
		RETURNING node_id::text`, name, node.String(), ttl).Scan(&holder)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// TaskStates is what is known of each task that has ever started.
func (s *Store) TaskStates(ctx context.Context) (map[domain.TaskKey]domain.TaskState, error) {
	rows, err := s.q.TaskState.WithContext(ctx).Find()
	if err != nil {
		return nil, err
	}
	states := make(map[domain.TaskKey]domain.TaskState, len(rows))
	for _, r := range rows {
		states[r.Key] = domain.TaskState{
			Started: r.StartedAt, Finished: deref(r.FinishedAt), Result: deref(r.Result), Error: deref(r.Error),
			Requested: deref(r.RequestedAt),
		}
	}
	return states, nil
}

// RequestTask asks for a task to run now. One that has never started is due already.
func (s *Store) RequestTask(ctx context.Context, key domain.TaskKey) error {
	t := s.q.TaskState
	_, err := t.WithContext(ctx).Where(t.Key.Eq(string(key))).Update(t.RequestedAt, time.Now())
	return err
}

func (s *Store) TaskStarted(ctx context.Context, key domain.TaskKey, at time.Time) error {
	return s.q.TaskState.WithContext(ctx).Save(&model.TaskState{Key: key, StartedAt: at})
}

func (s *Store) TaskFinished(ctx context.Context, key domain.TaskKey, at time.Time, result domain.TaskResult, runErr error) error {
	t := s.q.TaskState
	set := []field.AssignExpr{t.FinishedAt.Value(at), t.Result.Value(string(result)), t.Error.Null()}
	if runErr != nil {
		set[2] = t.Error.Value(runErr.Error())
	}
	_, err := t.WithContext(ctx).Where(t.Key.Eq(string(key))).UpdateSimple(set...)
	return err
}
