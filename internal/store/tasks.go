package store

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// HoldLease takes or renews the named lease for node, for ttl. It answers false while another
// node holds an unexpired lease. One statement does it, so two nodes asking at once cannot both
// win.
func (s *Store) HoldLease(ctx context.Context, name string, node uuid.UUID, ttl time.Duration) (bool, error) {
	var holder uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO leader (name, node_id, expires_at) VALUES ($1, $2, now() + $3)
		ON CONFLICT (name) DO UPDATE SET node_id = excluded.node_id, expires_at = excluded.expires_at
		WHERE leader.node_id = excluded.node_id OR leader.expires_at < now()
		RETURNING node_id`, name, node, ttl).Scan(&holder)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// TaskStates is what is known of each task that has ever started.
func (s *Store) TaskStates(ctx context.Context) (map[domain.TaskKey]domain.TaskState, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, started_at, finished_at, result, error, requested_at FROM task_state`)
	if err != nil {
		return nil, err
	}
	states := map[domain.TaskKey]domain.TaskState{}
	var r model.TaskState
	_, err = pgx.ForEachRow(rows, []any{&r.Key, &r.StartedAt, &r.FinishedAt, &r.Result, &r.Error, &r.RequestedAt}, func() error {
		states[r.Key] = domain.TaskState{
			Started: r.StartedAt, Finished: deref(r.FinishedAt), Result: deref(r.Result), Error: deref(r.Error),
			Requested: deref(r.RequestedAt),
		}
		return nil
	})
	return states, err
}

// RequestTask asks for a task to run now. One that has never started is due already.
func (s *Store) RequestTask(ctx context.Context, key domain.TaskKey) error {
	_, err := s.pool.Exec(ctx, `UPDATE task_state SET requested_at = $2 WHERE key = $1`, key, time.Now())
	return err
}

func (s *Store) TaskStarted(ctx context.Context, key domain.TaskKey, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO task_state (key, started_at) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET started_at = excluded.started_at,
			finished_at = NULL, result = NULL, error = NULL, requested_at = NULL`, key, at)
	return err
}

func (s *Store) TaskFinished(ctx context.Context, key domain.TaskKey, at time.Time, result domain.TaskResult, runErr error) error {
	var msg *string
	if runErr != nil {
		msg = new(runErr.Error())
	}
	_, err := s.pool.Exec(ctx, `UPDATE task_state SET finished_at = $2, result = $3, error = $4 WHERE key = $1`,
		key, at, result, msg)
	return err
}
