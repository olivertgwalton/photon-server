package store

import (
	"context"
	"errors"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

var (
	// ErrAskedFor is a title asked of a library while an earlier request for it is not declined.
	ErrAskedFor = errors.New("the title has been asked for already")
	// ErrAlreadyHeld is a title asked for that a library has come to hold since it was found.
	ErrAlreadyHeld = errors.New("a library holds the title already")
	// ErrDecided is a request withdrawn once an admin or manager has decided it, or decided again
	// unless it failed where it was sent.
	ErrDecided = errors.New("the request has been decided already")
	// ErrFilmSeasons is a film asked for by seasons.
	ErrFilmSeasons = errors.New("a film has no seasons: seasons are a show's")
)

// Request is a profile's request for a title a library of files' search found, by the id it was
// found under.
type Request struct {
	ID          uuid.UUID
	Profile     uuid.UUID
	ProfileName string
	Library     uuid.UUID
	Kind        domain.ItemKind
	Provider    domain.Provider
	Value       string
	Title       string
	Year        int
	// Seasons are a show's asked for; none is every one.
	Seasons []int
	State   domain.RequestState
	// Reason is why it was declined, where the one who declined it said.
	Reason string
	// DecidedBy is the admin or manager that approved or declined it; zero while it waits, or
	// where that profile has gone.
	DecidedBy uuid.UUID
	AskedAt   time.Time
	DecidedAt time.Time
	// Item is the title a library holds of it once it is available; zero before, or once that
	// title has gone.
	Item        uuid.UUID
	AvailableAt time.Time
}

type requestRow struct {
	ID          uuid.UUID
	ProfileID   uuid.UUID
	ProfileName string
	LibraryID   uuid.UUID
	Kind        domain.ItemKind
	Provider    domain.Provider
	Value       string
	Title       string
	Year        *int
	Seasons     []int
	State       domain.RequestState
	Reason      *string
	DecidedBy   *uuid.UUID
	AskedAt     time.Time
	DecidedAt   *time.Time
	ItemID      *uuid.UUID
	AvailableAt *time.Time
}

const requestColumns = `r.id, r.profile_id, p.name AS profile_name, r.library_id, r.kind, r.provider, r.value, r.title,
	r.year, r.seasons, r.state, r.reason, r.decided_by, r.asked_at, r.decided_at, r.item_id, r.available_at`

// Event is the request as an event of kind tells it.
func (r Request) Event(kind domain.EventKind) domain.Event {
	return domain.Event{Kind: kind, Profile: r.Profile, Library: r.Library, Item: r.Item, Details: domain.RequestDetails{
		RequestID: r.ID, State: r.State, Title: r.Title, Reason: r.Reason,
	}}
}

func (r requestRow) request() Request {
	return Request{
		ID: r.ID, Profile: r.ProfileID, ProfileName: r.ProfileName, Library: r.LibraryID, Kind: r.Kind,
		Provider: r.Provider, Value: r.Value, Title: r.Title, Year: deref(r.Year), Seasons: r.Seasons,
		State: r.State, Reason: deref(r.Reason), DecidedBy: deref(r.DecidedBy), AskedAt: r.AskedAt,
		DecidedAt: deref(r.DecidedAt), Item: deref(r.ItemID), AvailableAt: deref(r.AvailableAt),
	}
}

func (s *Store) request(ctx context.Context, q db, id uuid.UUID) (Request, error) {
	r, err := readRow[requestRow](ctx, q, `SELECT `+requestColumns+`
		FROM requests r JOIN profiles p ON p.id = r.profile_id WHERE r.id = $1`, id)
	return r.request(), err
}

// AskFor asks, for a profile, for a title a library of files' search found for it, of a show the
// seasons given, every one where none is: ErrNotFound for no such find of a library the profile
// may search. An admin's request is approved as it is asked, and sent where its library says.
func (s *Store) AskFor(ctx context.Context, profile, discovery uuid.UUID, seasons []int) (Request, error) {
	seasons = slices.Compact(slices.Sorted(slices.Values(seasons)))
	args := pgx.NamedArgs{"profile": profile, "id": discovery, "seasons": seasons}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var held bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM external_ids e JOIN items i ON i.id = e.item_id
				WHERE e.provider = d.provider AND e.value = d.value AND i.kind = d.kind) `+mayAsk, args).Scan(&held)
		if err != nil {
			return found(err)
		}
		if held {
			return ErrAlreadyHeld
		}
		var state domain.RequestState
		err = tx.QueryRow(ctx, `
			INSERT INTO requests (profile_id, library_id, kind, provider, value, title, year, seasons, state, decided_by, decided_at)
			SELECT p.id, d.library_id, d.kind, d.provider, d.value, d.title, d.year, @seasons,
				CASE p.role WHEN 'admin' THEN 'approved' ELSE 'pending' END,
				CASE p.role WHEN 'admin' THEN p.id END,
				CASE p.role WHEN 'admin' THEN now() END
			FROM discoveries d, profiles p WHERE d.id = @id AND p.id = @profile
			RETURNING id, state`, args).Scan(&id, &state)
		if err != nil || state != domain.RequestApproved {
			return err
		}
		return send(ctx, tx, id)
	})
	switch {
	case violates(err, uniqueViolation):
		return Request{}, ErrAskedFor
	case refusedBy(err, "request_seasons"):
		return Request{}, ErrFilmSeasons
	case err != nil:
		return Request{}, err
	}
	return s.request(ctx, s.pool, id)
}

// Requests answers a page of a profile's requests, newest first, and what the page tells of the
// rest.
func (s *Store) Requests(ctx context.Context, profile uuid.UUID, page Page) ([]Request, Extent, error) {
	return s.requests(ctx, `r.profile_id = @profile`, pgx.NamedArgs{"profile": profile}, page)
}

// RequestsKept answers a page of the requests of the profiles one administers, newest first, those
// in a state where one is given, and what the page tells of the rest: every profile's for an admin,
// by nil; the users a manager keeps for a manager.
func (s *Store) RequestsKept(ctx context.Context, by *uuid.UUID, state domain.RequestState, page Page) ([]Request, Extent, error) {
	return s.requests(ctx, `(@by::uuid IS NULL OR (p.managed_by = @by AND p.role = 'user'))
		AND (@state = '' OR r.state = @state)`,
		pgx.NamedArgs{"by": by, "state": string(state)}, page)
}

func (s *Store) requests(ctx context.Context, where string, args pgx.NamedArgs, page Page) ([]Request, Extent, error) {
	const from = ` FROM requests r JOIN profiles p ON p.id = r.profile_id WHERE `
	args["offset"], args["limit"] = page.Offset, page.Limit+1
	var rows []requestRow
	total, err := s.counted(ctx, page.Count, `SELECT count(*)`+from+where, args, func(ctx context.Context) (err error) {
		rows, err = queryStructs[requestRow](ctx, s.pool, `SELECT `+requestColumns+from+where+`
			ORDER BY r.asked_at DESC, r.id DESC OFFSET @offset LIMIT @limit`, args)
		return err
	})
	if err != nil {
		return nil, Extent{}, err
	}
	rows, more := pageOf(rows, page.Limit)
	out := make([]Request, len(rows))
	for i, r := range rows {
		out[i] = r.request()
	}
	return out, Extent{More: more, Total: total}, nil
}

// WithdrawRequest takes back a profile's request while it waits, answering it as it was:
// ErrDecided once it is decided, ErrNotFound for none of the profile's.
func (s *Store) WithdrawRequest(ctx context.Context, profile, id uuid.UUID) (Request, error) {
	r := Request{ID: id, Profile: profile}
	var library *uuid.UUID
	err := s.pool.QueryRow(ctx, `
		WITH gone AS (
			DELETE FROM requests WHERE id = $1 AND profile_id = $2 AND state = 'pending' RETURNING state, title, library_id
		)
		SELECT coalesce((SELECT state FROM gone), (SELECT state FROM requests WHERE id = $1 AND profile_id = $2), ''),
			coalesce((SELECT title FROM gone), ''), (SELECT library_id FROM gone)`,
		id, profile).Scan(&r.State, &r.Title, &library)
	switch {
	case err != nil:
		return Request{}, err
	case r.State == "":
		return Request{}, ErrNotFound
	case library == nil:
		return Request{}, ErrDecided
	}
	r.Library = *library
	return r, nil
}

// DecideRequest approves or declines a request while it waits, or once it failed where it was sent,
// as decider, for the reason given where it is declined: of any profile's for an admin, by nil, or
// of the users a manager keeps. One approved is sent where its library says. ErrNotFound for none
// of those, ErrDecided once it is decided otherwise.
func (s *Store) DecideRequest(ctx context.Context, id uuid.UUID, by *uuid.UUID, decider uuid.UUID, state domain.RequestState, reason string) (Request, error) {
	var was domain.RequestState
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			WITH kept AS (
				SELECT r.id, r.state FROM requests r JOIN profiles p ON p.id = r.profile_id
				WHERE r.id = $1 AND ($2::uuid IS NULL OR (p.managed_by = $2 AND p.role = 'user'))
			), decided AS (
				UPDATE requests r SET state = $4, reason = nullif($5, ''), decided_by = $3, decided_at = now()
				FROM kept WHERE r.id = kept.id AND kept.state IN ('pending', 'failed') RETURNING kept.state
			)
			SELECT coalesce((SELECT state FROM decided), (SELECT state FROM kept), '')`,
			id, by, decider, state, reason).Scan(&was)
		if err != nil || !decidable(was) || state != domain.RequestApproved {
			return err
		}
		return send(ctx, tx, id)
	})
	switch {
	case err != nil:
		return Request{}, err
	case was == "":
		return Request{}, ErrNotFound
	case !decidable(was):
		return Request{}, ErrDecided
	}
	return s.request(ctx, s.pool, id)
}

func decidable(s domain.RequestState) bool {
	return s == domain.RequestPending || s == domain.RequestFailed
}

// send queues an approved request to be sent where its library's requests go, if anywhere.
func send(ctx context.Context, tx db, id uuid.UUID) error {
	var sends bool
	err := tx.QueryRow(ctx, `
		SELECT l.request_source IS NOT NULL FROM requests r JOIN libraries l ON l.id = r.library_id WHERE r.id = $1`,
		id).Scan(&sends)
	if err != nil || !sends {
		return err
	}
	return enqueue(ctx, tx, domain.JobSendRequest, id)
}

// Sending is an approved request as it is sent: the title it wants, and the library whose requests
// go somewhere. False for a request decided otherwise since, or whose library now sends none.
func (s *Store) Sending(ctx context.Context, id uuid.UUID) (domain.Wanted, domain.Library, bool, error) {
	r, err := s.request(ctx, s.pool, id)
	if errors.Is(err, ErrNotFound) || err == nil && r.State != domain.RequestApproved {
		return domain.Wanted{}, domain.Library{}, false, nil
	}
	if err != nil {
		return domain.Wanted{}, domain.Library{}, false, err
	}
	lib, err := s.Library(ctx, r.Library)
	if err != nil || lib.RequestSource == "" {
		return domain.Wanted{}, domain.Library{}, false, err
	}
	w := domain.Wanted{ID: r.ID, Kind: r.Kind, IDs: map[domain.Provider]string{r.Provider: r.Value}, Title: r.Title, Year: r.Year, Seasons: r.Seasons}
	return w, lib, true, nil
}

// SentRequest records where an approved request was sent: sent, or failed, saying why, answering
// it as it now is. A request decided otherwise meanwhile, or taken back, is left as it is, and
// answered false.
func (s *Store) SentRequest(ctx context.Context, id uuid.UUID, state domain.RequestState, reason string) (Request, bool, error) {
	err := affected(s.pool.Exec(ctx, `UPDATE requests SET state = $2, reason = nullif($3, '') WHERE id = $1 AND state = 'approved'`, id, state, reason))
	if err == nil {
		var r Request
		r, err = s.request(ctx, s.pool, id)
		if err == nil {
			return r, true, nil
		}
	}
	if errors.Is(err, ErrNotFound) {
		return Request{}, false, nil
	}
	return Request{}, false, err
}
