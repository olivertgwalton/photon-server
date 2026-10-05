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

// TaskStarts is when each task last started.
func (s *Store) TaskStarts(ctx context.Context) (map[domain.TaskKey]time.Time, error) {
	rows, err := s.q.TaskState.WithContext(ctx).Find()
	if err != nil {
		return nil, err
	}
	starts := make(map[domain.TaskKey]time.Time, len(rows))
	for _, r := range rows {
		starts[r.Key] = r.StartedAt
	}
	return starts, nil
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
