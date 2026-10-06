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

// enqueue adds a job inside the transaction whose write made it necessary, so the job and its
// cause commit together. A job already queued for the subject stands; one running will run again
// once it ends, since it may have read the subject before this write; a dead one gets a fresh
// set of attempts, as its subject has changed.
func enqueue(ctx context.Context, tx *query.Query, kind domain.JobKind, subject model.UUID) error {
	return enqueueAfter(ctx, tx, kind, subject, 0)
}

// enqueueAfter is enqueue for a job due after a quiet delay: asking again before it is due moves it
// later, so a burst of causes is answered once, when the burst ends.
func enqueueAfter(ctx context.Context, tx *query.Query, kind domain.JobKind, subject model.UUID, delay time.Duration) error {
	return insertJob(ctx, tx, kind, subject, delay, 0)
}

// askedPriority is a job an admin asked for by hand: it is claimed before everything a schedule
// or a scan queued.
const askedPriority = 1

// indexPriority is a part's keyframes job: an import queues thousands, so they are claimed after
// the other analysis queued with them.
const indexPriority = -1

// refreshPriority is a title's scheduled match: claimed after the titles a scan has just found,
// which have no match at all yet.
const refreshPriority = -1

// enqueueAsked is enqueue for a job an admin is waiting on.
func enqueueAsked(ctx context.Context, tx *query.Query, kind domain.JobKind, subject model.UUID) error {
	return insertJob(ctx, tx, kind, subject, 0, askedPriority)
}

// requeue ends an INSERT of jobs as enqueue answers a job already there, keeping its priority.
const requeue = `
	ON CONFLICT (kind, subject) DO UPDATE SET
		state = CASE jobs.state WHEN 'running' THEN 'rerun' WHEN 'dead' THEN 'queued' ELSE jobs.state END,
		attempts = CASE jobs.state WHEN 'dead' THEN 0 ELSE jobs.attempts END`

func insertJob(ctx context.Context, tx *query.Query, kind domain.JobKind, subject model.UUID, delay time.Duration, priority int16) error {
	// GORM refuses expressions in an upsert's assignments.
	return tx.Job.WithContext(ctx).UnderlyingDB().Exec(`
		INSERT INTO jobs (kind, subject, run_after, priority) VALUES (?, ?, now() + ?::interval, ?)
		ON CONFLICT (kind, subject) DO UPDATE SET
			state = CASE jobs.state WHEN 'running' THEN 'rerun' WHEN 'dead' THEN 'queued' ELSE jobs.state END,
			attempts = CASE jobs.state WHEN 'dead' THEN 0 ELSE jobs.attempts END,
			run_after = CASE WHEN jobs.state IN ('queued', 'dead') THEN excluded.run_after ELSE jobs.run_after END,
			priority = greatest(jobs.priority, excluded.priority)`,
		kind, subject, delay.String(), priority).Error
}

// ScanLibrary asks for a library to be scanned once delay has passed with no further asking.
func (s *Store) ScanLibrary(ctx context.Context, lib uuid.UUID, delay time.Duration) error {
	return s.ScanFolders(ctx, lib, []string{"."}, delay)
}

// ScanFolders asks for folders of a library, and everything under them, to be scanned once delay
// has passed with no further asking.
func (s *Store) ScanFolders(ctx context.Context, lib uuid.UUID, folders []string, delay time.Duration) error {
	return s.q.Transaction(func(tx *query.Query) error {
		return askScan(ctx, tx, model.UUID(lib), folders, delay)
	})
}

func askScan(ctx context.Context, tx *query.Query, lib model.UUID, folders []string, delay time.Duration) error {
	for _, f := range folders {
		err := tx.Job.WithContext(ctx).UnderlyingDB().Exec(`
			INSERT INTO scan_requests (library_id, path) VALUES (?, ?)
			ON CONFLICT (library_id, path) DO UPDATE SET asked_at = now()`, lib, f).Error
		if err != nil {
			return err
		}
	}
	return enqueueAfter(ctx, tx, domain.JobScanLibrary, lib, delay)
}

// ScanRequests answers the folders a library's scan has been asked to read, and when they were
// read, for ScanAnswered.
func (s *Store) ScanRequests(ctx context.Context, lib uuid.UUID) (folders []string, read time.Time, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT now(), coalesce(array_agg(path ORDER BY path), '{}') FROM scan_requests WHERE library_id = $1`,
		lib.String()).Scan(&read, &folders)
	return folders, read, err
}

// ScanAnswered forgets the requests a scan has answered: those it read, unless asked again since.
func (s *Store) ScanAnswered(ctx context.Context, lib uuid.UUID, folders []string, read time.Time) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM scan_requests WHERE library_id = $1 AND path = ANY($2) AND asked_at <= $3`,
		lib.String(), folders, read)
	return err
}

// ClaimJobs leases up to limit queued jobs of the given kinds to node. Workers that ask together
// never receive the same job: each row is taken by one transaction and skipped by the others.
func (s *Store) ClaimJobs(ctx context.Context, kinds []domain.JobKind, node uuid.UUID, lease time.Duration, limit int) ([]domain.Job, error) {
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
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Job, error) {
		var j domain.Job
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
// marks it dead once it has had maxAttempts and its subject has not changed since it was claimed,
// answering whether it did.
func (s *Store) FailJob(ctx context.Context, job domain.Job, runErr error) (bool, error) {
	j := s.q.Job
	q := j.WithContext(ctx).Where(j.ID.Eq(job.ID))
	if job.Attempts >= maxAttempts {
		dead, err := j.WithContext(ctx).Where(j.ID.Eq(job.ID), j.State.Eq(string(domain.JobRunning))).
			UpdateSimple(j.State.Value(string(domain.JobDead)), j.LeaseUntil.Null(), j.LastError.Value(runErr.Error()))
		if err != nil || dead.RowsAffected > 0 {
			return err == nil, err
		}
	}
	backoff := min(time.Minute<<job.Attempts, time.Hour)
	_, err := q.UpdateSimple(
		j.State.Value(string(domain.JobQueued)), j.LeaseUntil.Null(), j.NodeID.Null(),
		j.RunAfter.Value(time.Now().Add(backoff)), j.LastError.Value(runErr.Error()),
	)
	return false, err
}

// PostponeJob queues a job again once a delay has passed, giving back its attempt: it could not
// start for want of room on its node, which is no fault of its subject's.
func (s *Store) PostponeJob(ctx context.Context, job domain.Job, delay time.Duration) error {
	j := s.q.Job
	_, err := j.WithContext(ctx).Where(j.ID.Eq(job.ID)).UpdateSimple(
		j.State.Value(string(domain.JobQueued)), j.Attempts.Sub(1), j.LeaseUntil.Null(), j.NodeID.Null(),
		j.RunAfter.Value(time.Now().Add(delay)))
	return err
}

// RunningJobs answers the jobs being run now, on every node, the oldest first.
func (s *Store) RunningJobs(ctx context.Context) ([]domain.Job, error) {
	j := s.q.Job
	rows, err := j.WithContext(ctx).Where(j.State.In(string(domain.JobRunning), string(domain.JobRerun))).Order(j.ID).Find()
	if err != nil {
		return nil, err
	}
	out := make([]domain.Job, len(rows))
	for i, r := range rows {
		out[i] = domain.Job{ID: r.ID, Kind: r.Kind, Subject: uuid.UUID(r.Subject), Attempts: int(r.Attempts)}
	}
	return out, nil
}

// JobsLeft answers how many jobs of each kind are left to run, queued or running, leaving out
// kinds with none.
func (s *Store) JobsLeft(ctx context.Context) (map[domain.JobKind]int, error) {
	j := s.q.Job
	var counts []JobCount
	err := j.WithContext(ctx).Select(j.Kind, j.ID.Count().As("count")).
		Where(j.State.In(string(domain.JobQueued), string(domain.JobRunning), string(domain.JobRerun))).
		Group(j.Kind).Scan(&counts)
	out := make(map[domain.JobKind]int, len(counts))
	for _, c := range counts {
		out[c.Kind] = c.Count
	}
	return out, err
}

// ExtendLease keeps a running job's lease while node is at it.
func (s *Store) ExtendLease(ctx context.Context, id int64, node uuid.UUID, lease time.Duration) error {
	j := s.q.Job
	info, err := j.WithContext(ctx).
		Where(j.ID.Eq(id), j.State.In(string(domain.JobRunning), string(domain.JobRerun)), j.NodeID.Eq(model.UUID(node))).
		UpdateSimple(j.LeaseUntil.Value(time.Now().Add(lease)))
	if err == nil && info.RowsAffected == 0 {
		return domain.ErrLeaseLost
	}
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

// SaveKeyframes records a part's keyframe times, none where it has none known. pgx writes them as
// one bigint[].
func (s *Store) SaveKeyframes(ctx context.Context, part uuid.UUID, ptsMS []int64) error {
	if ptsMS == nil {
		ptsMS = []int64{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO keyframes (part_id, pts_ms) VALUES ($1, $2)
		ON CONFLICT (part_id) DO UPDATE SET pts_ms = excluded.pts_ms`, part.String(), ptsMS)
	return err
}

// rekeyframe queues a library's parts with no keyframes known to be read again under its new mode;
// those already known stay, since every mode finds the same ones.
func rekeyframe(ctx context.Context, tx *query.Query, lib model.UUID, mode domain.KeyframeMode) error {
	if mode == domain.KeyframesOff {
		return nil
	}
	db := tx.Job.WithContext(ctx).UnderlyingDB()
	err := db.Exec(`
		DELETE FROM keyframes k USING parts p, versions v
		WHERE k.part_id = p.id AND v.id = p.version_id AND v.library_id = ? AND cardinality(k.pts_ms) = 0`, lib).Error
	if err != nil {
		return err
	}
	return db.Exec(`
		INSERT INTO jobs (kind, subject, priority)
		SELECT 'keyframes', p.id, ? FROM parts p JOIN versions v ON v.id = p.version_id
		WHERE v.library_id = ?
			AND EXISTS (SELECT 1 FROM part_files f WHERE f.part_id = p.id)
			AND EXISTS (SELECT 1 FROM streams s WHERE s.part_id = p.id AND s.kind = 'video')
			AND NOT EXISTS (SELECT 1 FROM keyframes k WHERE k.part_id = p.id)`+requeue, indexPriority, lib).Error
}

// AskKeyframes queues a part's keyframes job ahead of the rest, for a part played before its turn.
func (s *Store) AskKeyframes(ctx context.Context, part uuid.UUID) error {
	return enqueueAsked(ctx, s.q, domain.JobKeyframes, model.UUID(part))
}

// JobCount is how many jobs of a kind are in a state.
type JobCount struct {
	Kind  domain.JobKind
	State domain.JobState
	Count int
}

// DeadJob is a job that failed every attempt, and why it last did.
type DeadJob struct {
	domain.Job
	Error string
}

// deadShown is how many dead jobs an admin is shown, the most recent first.
const deadShown = 50

// JobQueue answers how many jobs of each kind are in each state, and the jobs that are dead.
func (s *Store) JobQueue(ctx context.Context) ([]JobCount, []DeadJob, error) {
	j := s.q.Job
	var counts []JobCount
	if err := j.WithContext(ctx).Select(j.Kind, j.State, j.ID.Count().As("count")).
		Group(j.Kind, j.State).Order(j.Kind, j.State).Scan(&counts); err != nil {
		return nil, nil, err
	}
	rows, err := j.WithContext(ctx).Where(j.State.Eq(string(domain.JobDead))).Order(j.ID.Desc()).Limit(deadShown).Find()
	if err != nil {
		return nil, nil, err
	}
	dead := make([]DeadJob, len(rows))
	for i, r := range rows {
		dead[i] = DeadJob{ID: r.ID, Kind: r.Kind, Subject: uuid.UUID(r.Subject), Attempts: int(r.Attempts), Error: deref(r.LastError)}
	}
	return counts, dead, nil
}

// RetryJob gives a dead job a fresh set of attempts now. ErrNotFound for no dead job of that id.
func (s *Store) RetryJob(ctx context.Context, id int64) error {
	j := s.q.Job
	res, err := j.WithContext(ctx).Where(j.ID.Eq(id), j.State.Eq(string(domain.JobDead))).
		UpdateSimple(j.State.Value(string(domain.JobQueued)), j.Attempts.Value(0), j.RunAfter.Value(time.Now()))
	if err == nil && res.RowsAffected == 0 {
		err = ErrNotFound
	}
	return err
}

// Identified records that a title has just been matched on every provider its library takes.
func (s *Store) Identified(ctx context.Context, id uuid.UUID) error {
	i := s.q.Item
	_, err := i.WithContext(ctx).Where(i.ID.Eq(model.UUID(id))).Update(i.IdentifiedAt, time.Now())
	return err
}

// RefreshStale queues a match of every film and show its library refreshes and that was last
// matched longer ago than the library says, as Jellyfin's scheduled metadata refresh does. It
// answers how many.
func (s *Store) RefreshStale(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO jobs (kind, subject, priority)
		SELECT 'identify', i.id, $1 FROM items i JOIN libraries l ON l.id = i.library_id
		WHERE i.kind IN ('movie', 'show') AND l.refresh_days > 0
			AND coalesce(i.identified_at, '-infinity') < now() - make_interval(days => l.refresh_days)`+requeue, refreshPriority)
	return tag.RowsAffected(), err
}
