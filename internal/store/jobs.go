package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// maxAttempts is how often a job is tried before it is dead and waits for its subject to change.
const maxAttempts = 5

// enqueue adds a job due now inside the transaction whose write made it necessary, so the job and
// its cause commit together. A job already queued for the subject stands, due now if it was due in
// the window; one running will run again once it ends, since it may have read the subject before
// this write; a dead one gets a fresh set of attempts, as its subject has changed.
func enqueue(ctx context.Context, tx db, kind domain.JobKind, subject uuid.UUID) error {
	return enqueueAfter(ctx, tx, kind, subject, 0)
}

// enqueueAfter is enqueue for a job due after a quiet delay: asking again before it is due moves it
// later, so a burst of causes is answered once, when the burst ends.
func enqueueAfter(ctx context.Context, tx db, kind domain.JobKind, subject uuid.UUID, delay time.Duration) error {
	return insertJob(ctx, tx, kind, subject, delay, 0, domain.JobDueNow)
}

// addedDue is when the work a scan queues for what it added is due under its timing: now where it
// is done as parts are added, else in the window, as Plex leaves a new item's to maintenance where
// it is set to a scheduled task alone. A timing changed later leaves queued jobs as they are, as
// Plex decides as an item is added.
func addedDue(t domain.Timing) domain.JobDue {
	switch t {
	case domain.TimingWindow:
		return domain.JobDueWindow
	case domain.TimingWindowAndAdded:
	}
	return domain.JobDueNow
}

// promoteFor makes every job of kind left to run due now where due is now, as an admin asking for a
// task has its whole backlog run, to its end, rather than in the window.
func promoteFor(ctx context.Context, tx db, kind domain.JobKind, due domain.JobDue) error {
	switch due {
	case domain.JobDueNow:
	case domain.JobDueWindow:
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE jobs SET due = 'now' WHERE kind = $1 AND due = 'window' AND state IN ('queued', 'running', 'rerun')`, kind)
	return err
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
func enqueueAsked(ctx context.Context, tx db, kind domain.JobKind, subject uuid.UUID) error {
	return insertJob(ctx, tx, kind, subject, 0, askedPriority, domain.JobDueNow)
}

// requeue ends an INSERT of jobs as enqueue answers a job already there, keeping its priority.
const requeue = `
	ON CONFLICT (kind, subject) DO UPDATE SET
		state = CASE jobs.state WHEN 'running' THEN 'rerun' WHEN 'dead' THEN 'queued' ELSE jobs.state END,
		attempts = CASE jobs.state WHEN 'dead' THEN 0 ELSE jobs.attempts END`

// insertJob queues a job due as said. Asking again may bring a job due in the window forward to
// now, never put one due now back to the window; a dead one takes what it is asked for afresh.
func insertJob(ctx context.Context, tx db, kind domain.JobKind, subject uuid.UUID, delay time.Duration, priority int16, due domain.JobDue) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO jobs (kind, subject, run_after, priority, due) VALUES ($1, $2, now() + $3, $4, $5)
		ON CONFLICT (kind, subject) DO UPDATE SET
			state = CASE jobs.state WHEN 'running' THEN 'rerun' WHEN 'dead' THEN 'queued' ELSE jobs.state END,
			attempts = CASE jobs.state WHEN 'dead' THEN 0 ELSE jobs.attempts END,
			run_after = CASE WHEN jobs.state IN ('queued', 'dead') THEN excluded.run_after ELSE jobs.run_after END,
			priority = greatest(jobs.priority, excluded.priority),
			due = CASE WHEN jobs.state = 'dead' OR excluded.due = 'now' THEN excluded.due ELSE jobs.due END`,
		kind, subject, delay, priority, due)
	return err
}

// ScanLibrary asks for a library to be scanned once delay has passed with no further asking.
func (s *Store) ScanLibrary(ctx context.Context, lib uuid.UUID, delay time.Duration) error {
	return s.ScanFolders(ctx, lib, []string{"."}, delay)
}

// ScanFolders asks for folders of a library, and everything under them, to be scanned once delay
// has passed with no further asking.
func (s *Store) ScanFolders(ctx context.Context, lib uuid.UUID, folders []string, delay time.Duration) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return askScan(ctx, tx, lib, folders, delay)
	})
}

func askScan(ctx context.Context, tx db, lib uuid.UUID, folders []string, delay time.Duration) error {
	for _, f := range folders {
		_, err := tx.Exec(ctx, `
			INSERT INTO scan_requests (library_id, path) VALUES ($1, $2)
			ON CONFLICT (library_id, path) DO UPDATE SET asked_at = now()`, lib, f)
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
		lib).Scan(&read, &folders)
	return folders, read, err
}

// ScanAnswered forgets the requests a scan has answered: those it read, unless asked again since.
func (s *Store) ScanAnswered(ctx context.Context, lib uuid.UUID, folders []string, read time.Time) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM scan_requests WHERE library_id = $1 AND path = ANY($2) AND asked_at <= $3`,
		lib, folders, read)
	return err
}

// ClaimJobs leases up to limit queued jobs of the given kinds to node, of those in nowOnly only the
// ones due now. Workers that ask together
// never receive the same job: each row is taken by one transaction and skipped by the others. The
// rows are picked once, in a materialized CTE: as `id IN (SELECT … SKIP LOCKED LIMIT n)` the
// planner may run the subquery again for each row it scans, each run skipping what the last
// locked, and lease far more than n.
func (s *Store) ClaimJobs(ctx context.Context, kinds, nowOnly []domain.JobKind, node uuid.UUID, lease time.Duration, limit int) ([]domain.Job, error) {
	rows, err := s.pool.Query(ctx, `
		WITH picked AS MATERIALIZED (
			SELECT id FROM jobs WHERE state = 'queued' AND run_after <= now() AND kind = ANY($1)
				AND (due = 'now' OR NOT kind = ANY(coalesce($5::text[], '{}')))
			ORDER BY priority DESC, id FOR UPDATE SKIP LOCKED LIMIT $4)
		UPDATE jobs SET state = 'running', lease_until = now() + $2, attempts = attempts + 1, node_id = $3
		FROM picked WHERE jobs.id = picked.id
		RETURNING jobs.id, jobs.kind, jobs.subject, jobs.attempts, jobs.due`, kinds, lease, node, limit, nowOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[domain.Job])
}

// CompleteJob removes a finished job, as what it produced is the record that it ran, or queues
// it again if its subject changed while it ran.
func (s *Store) CompleteJob(ctx context.Context, id int64) error {
	done, err := s.pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1 AND state = 'running'`, id)
	if err != nil || done.RowsAffected() > 0 {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE jobs SET state = 'queued', attempts = 0, lease_until = NULL, node_id = NULL WHERE id = $1`, id)
	return err
}

// FailJob queues a job again after a backoff that doubles with each attempt, up to an hour, or
// marks it dead once it has had maxAttempts and its subject has not changed since it was claimed,
// answering whether it did.
func (s *Store) FailJob(ctx context.Context, job domain.Job, runErr error) (bool, error) {
	if job.Attempts >= maxAttempts {
		dead, err := s.pool.Exec(ctx, `
			UPDATE jobs SET state = 'dead', lease_until = NULL, last_error = $2 WHERE id = $1 AND state = 'running'`,
			job.ID, runErr.Error())
		if err != nil || dead.RowsAffected() > 0 {
			return err == nil, err
		}
	}
	backoff := min(time.Minute<<job.Attempts, time.Hour)
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET state = 'queued', lease_until = NULL, node_id = NULL, run_after = now() + $2, last_error = $3
		WHERE id = $1`, job.ID, backoff, runErr.Error())
	return false, err
}

// PostponeJob queues a job again once a delay has passed, giving back its attempt: it could not
// start for want of room on its node, which is no fault of its subject's.
func (s *Store) PostponeJob(ctx context.Context, job domain.Job, delay time.Duration) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET state = 'queued', attempts = attempts - 1, lease_until = NULL, node_id = NULL,
			run_after = now() + $2
		WHERE id = $1`, job.ID, delay)
	return err
}

// RunningJobs answers the jobs being run now, on every node, the oldest first.
func (s *Store) RunningJobs(ctx context.Context) ([]domain.Job, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, subject, attempts, due FROM jobs WHERE state IN ('running', 'rerun') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[domain.Job])
}

// JobsLeft answers how many jobs of each kind are left to run, queued or running, leaving out
// kinds with none.
func (s *Store) JobsLeft(ctx context.Context) (map[domain.JobKind]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, count(*) FROM jobs WHERE state IN ('queued', 'running', 'rerun') GROUP BY kind`)
	if err != nil {
		return nil, err
	}
	out := map[domain.JobKind]int{}
	var kind domain.JobKind
	var count int
	_, err = pgx.ForEachRow(rows, []any{&kind, &count}, func() error {
		out[kind] = count
		return nil
	})
	return out, err
}

// AnyJobsLeft answers whether any job of kind is left to run, queued or running.
func (s *Store) AnyJobsLeft(ctx context.Context, kind domain.JobKind) (bool, error) {
	var left bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM jobs WHERE kind = $1 AND state IN ('queued', 'running', 'rerun'))`,
		kind).Scan(&left)
	return left, err
}

// ExtendLease keeps a running job's lease while node is at it.
func (s *Store) ExtendLease(ctx context.Context, id int64, node uuid.UUID, lease time.Duration) error {
	info, err := s.pool.Exec(ctx, `
		UPDATE jobs SET lease_until = now() + $3 WHERE id = $1 AND state IN ('running', 'rerun') AND node_id = $2`,
		id, node, lease)
	if err == nil && info.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return err
}

// SweepJobs queues again every job whose worker's lease ran out: the worker died or lost touch.
func (s *Store) SweepJobs(ctx context.Context) (int64, error) {
	info, err := s.pool.Exec(ctx, `
		UPDATE jobs SET state = 'queued', lease_until = NULL, node_id = NULL
		WHERE state IN ('running', 'rerun') AND lease_until < now()`)
	return info.RowsAffected(), err
}

// PartFile is a place a part's bytes are: its library's root and the path inside it. Of several
// identical copies, any will do.
func (s *Store) PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT l.root, f.rel_path FROM part_files f JOIN libraries l ON l.id = f.library_id
		WHERE f.part_id = $1 ORDER BY f.rel_path LIMIT 1`, part).Scan(&root, &rel)
	return root, rel, found(err)
}

// SaveKeyframes records a part's keyframe times, none where it has none known. pgx writes them as
// one bigint[].
func (s *Store) SaveKeyframes(ctx context.Context, part uuid.UUID, ptsMS []int64) error {
	if ptsMS == nil {
		ptsMS = []int64{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO keyframes (part_id, pts_ms) VALUES ($1, $2)
		ON CONFLICT (part_id) DO UPDATE SET pts_ms = excluded.pts_ms`, part, ptsMS)
	return err
}

// rekeyframe queues a library's parts with no keyframes known to be read again under its new mode;
// those already known stay, since every mode finds the same ones.
func rekeyframe(ctx context.Context, tx db, lib uuid.UUID, mode domain.KeyframeMode) error {
	if mode == domain.KeyframesOff {
		return nil
	}
	_, err := tx.Exec(ctx, `
		DELETE FROM keyframes k USING parts p, versions v
		WHERE k.part_id = p.id AND v.id = p.version_id AND v.library_id = $1 AND cardinality(k.pts_ms) = 0`, lib)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO jobs (kind, subject, priority)
		SELECT 'keyframes', p.id, $1 FROM parts p JOIN versions v ON v.id = p.version_id
		WHERE v.library_id = $2
			AND EXISTS (SELECT 1 FROM part_files f WHERE f.part_id = p.id)
			AND EXISTS (SELECT 1 FROM streams s WHERE s.part_id = p.id AND s.kind = 'video')
			AND NOT EXISTS (SELECT 1 FROM keyframes k WHERE k.part_id = p.id)`+requeue, indexPriority, lib)
	return err
}

// QueueKeyframeWalk queues a part with no keyframe index to be walked through for them in the
// maintenance window, after the other analysis, as its keyframes job is.
func (s *Store) QueueKeyframeWalk(ctx context.Context, part uuid.UUID) error {
	return insertJob(ctx, s.pool, domain.JobKeyframeWalk, part, 0, indexPriority, domain.JobDueWindow)
}

// AskKeyframes queues a part's keyframes job ahead of the rest, for a part played before its turn.
func (s *Store) AskKeyframes(ctx context.Context, part uuid.UUID) error {
	return enqueueAsked(ctx, s.pool, domain.JobKeyframes, part)
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
	rows, err := s.pool.Query(ctx, `SELECT kind, state, count(*) FROM jobs GROUP BY kind, state ORDER BY kind, state`)
	if err != nil {
		return nil, nil, err
	}
	counts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[JobCount])
	if err != nil {
		return nil, nil, err
	}
	rows, err = s.pool.Query(ctx, `
		SELECT id, kind, subject, attempts, coalesce(last_error, '') FROM jobs WHERE state = 'dead'
		ORDER BY id DESC LIMIT $1`, deadShown)
	if err != nil {
		return nil, nil, err
	}
	dead, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (DeadJob, error) {
		var d DeadJob
		err := r.Scan(&d.ID, &d.Kind, &d.Subject, &d.Attempts, &d.Error)
		return d, err
	})
	return counts, dead, err
}

// RetryJob gives a dead job a fresh set of attempts now. ErrNotFound for no dead job of that id.
func (s *Store) RetryJob(ctx context.Context, id int64) error {
	return affected(s.pool.Exec(ctx, `
		UPDATE jobs SET state = 'queued', attempts = 0, run_after = now() WHERE id = $1 AND state = 'dead'`, id))
}

// Identified records that a title has just been matched on every provider its library takes, and
// asks ThemerrDB for its theme where its library takes those, so a refresh finds one listed since.
func (s *Store) Identified(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE items SET identified_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return askThemes(ctx, tx, `@id`, pgx.NamedArgs{"id": id})
	})
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
