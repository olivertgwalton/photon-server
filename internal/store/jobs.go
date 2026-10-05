package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// maxAttempts is how often a job is tried before it is dead and waits for its subject to change.
const maxAttempts = 5

type Job struct {
	ID       int64
	Kind     domain.JobKind
	Subject  uuid.UUID
	Attempts int
}

// enqueue adds a job inside the transaction whose write made it necessary, so the job and its
// cause commit together. A job already queued for the subject stands; one running will run again
// once it ends, since it may have read the subject before this write; a dead one gets a fresh
// set of attempts, as its subject has changed.
func enqueue(ctx context.Context, tx *query.Query, kind domain.JobKind, subject model.UUID) error {
	// GORM refuses expressions in an upsert's assignments.
	return tx.Job.WithContext(ctx).UnderlyingDB().Exec(`
		INSERT INTO jobs (kind, subject) VALUES (?, ?)
		ON CONFLICT (kind, subject) DO UPDATE SET
			state = CASE jobs.state WHEN 'running' THEN 'rerun' WHEN 'dead' THEN 'queued' ELSE jobs.state END,
			attempts = CASE jobs.state WHEN 'dead' THEN 0 ELSE jobs.attempts END`,
		kind, subject).Error
}

// ClaimJobs leases up to limit queued jobs of the given kinds to node. Workers that ask together
// never receive the same job: each row is taken by one transaction and skipped by the others.
func (s *Store) ClaimJobs(ctx context.Context, kinds []domain.JobKind, node uuid.UUID, lease time.Duration, limit int) ([]Job, error) {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = string(k)
	}
	rows, err := s.pool.Query(ctx, `
		UPDATE jobs SET state = 'running', lease_until = now() + $2, attempts = attempts + 1, node_id = $3
		WHERE id IN (
			SELECT id FROM jobs WHERE state = 'queued' AND run_after <= now() AND kind = ANY($1)
			ORDER BY priority DESC, id FOR UPDATE SKIP LOCKED LIMIT $4)
		RETURNING id, kind, subject::text, attempts`, names, lease, node.String(), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Job, error) {
		var j Job
		var kind, subject string
		if err := r.Scan(&j.ID, &kind, &subject, &j.Attempts); err != nil {
			return j, err
		}
		j.Kind = domain.JobKind(kind)
		j.Subject, err = uuid.Parse(subject)
		return j, err
	})
}

// CompleteJob removes a finished job, as what it produced is the record that it ran, or queues
// it again if its subject changed while it ran.
func (s *Store) CompleteJob(ctx context.Context, id int64) error {
	j := s.q.Job
	done, err := j.WithContext(ctx).Where(j.ID.Eq(id), j.State.Eq(string(domain.JobRunning))).Delete()
	if err != nil || done.RowsAffected > 0 {
		return err
	}
	_, err = j.WithContext(ctx).Where(j.ID.Eq(id)).UpdateSimple(
		j.State.Value(string(domain.JobQueued)), j.Attempts.Value(0), j.LeaseUntil.Null(), j.NodeID.Null())
	return err
}

// FailJob queues a job again after a backoff that doubles with each attempt, up to an hour, or
// marks it dead once it has had maxAttempts and its subject has not changed since it was claimed.
func (s *Store) FailJob(ctx context.Context, job Job, runErr error) error {
	j := s.q.Job
	q := j.WithContext(ctx).Where(j.ID.Eq(job.ID))
	if job.Attempts >= maxAttempts {
		dead, err := j.WithContext(ctx).Where(j.ID.Eq(job.ID), j.State.Eq(string(domain.JobRunning))).
			UpdateSimple(j.State.Value(string(domain.JobDead)), j.LeaseUntil.Null(), j.LastError.Value(runErr.Error()))
		if err != nil || dead.RowsAffected > 0 {
			return err
		}
	}
	backoff := min(time.Minute<<job.Attempts, time.Hour)
	_, err := q.UpdateSimple(
		j.State.Value(string(domain.JobQueued)), j.LeaseUntil.Null(), j.NodeID.Null(),
		j.RunAfter.Value(time.Now().Add(backoff)), j.LastError.Value(runErr.Error()),
	)
	return err
}

// SweepJobs queues again every job whose worker's lease ran out: the worker died or lost touch.
func (s *Store) SweepJobs(ctx context.Context) (int64, error) {
	j := s.q.Job
	info, err := j.WithContext(ctx).
		Where(j.State.In(string(domain.JobRunning), string(domain.JobRerun)), j.LeaseUntil.Lt(time.Now())).
		UpdateSimple(j.State.Value(string(domain.JobQueued)), j.LeaseUntil.Null(), j.NodeID.Null())
	return info.RowsAffected, err
}

// PartFile is a place a part's bytes are: its library's root and the path inside it. Of several
// identical copies, any will do.
func (s *Store) PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error) {
	f, l := s.q.PartFile, s.q.Library
	var row struct {
		Root    string
		RelPath string
	}
	err = f.WithContext(ctx).Select(l.Root, f.RelPath).Join(l, l.ID.EqCol(f.LibraryID)).
		Where(f.PartID.Eq(model.UUID(part))).Order(f.RelPath).Limit(1).Scan(&row)
	if err == nil && row.RelPath == "" {
		err = ErrNotFound
	}
	return row.Root, row.RelPath, err
}

// SaveKeyframes records a part's keyframe times. pgx writes them as one bigint[].
func (s *Store) SaveKeyframes(ctx context.Context, part uuid.UUID, ptsMS []int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO keyframes (part_id, pts_ms) VALUES ($1, $2)
		ON CONFLICT (part_id) DO UPDATE SET pts_ms = excluded.pts_ms`, part.String(), ptsMS)
	return err
}
