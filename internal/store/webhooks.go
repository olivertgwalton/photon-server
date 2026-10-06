package store

import (
	"context"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Webhook is an address told of the events it asked for. Its secret is not here: it is answered
// once, by AddWebhook's caller, and read again only to sign.
type Webhook struct {
	ID        uuid.UUID
	URL       string
	Events    []domain.EventKind
	CreatedAt time.Time
}

func (s *Store) AddWebhook(ctx context.Context, url string, kinds []domain.EventKind, secret string) (Webhook, error) {
	out := Webhook{URL: url, Events: kinds}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO webhooks (url, secret) VALUES ($1, $2) RETURNING id, created_at`, url, secret).
			Scan(&out.ID, &out.CreatedAt)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO webhook_events (webhook_id, kind) SELECT $1, unnest($2::text[])`, out.ID, kinds)
		return err
	})
	return out, err
}

// Webhooks answers every webhook, the oldest first.
func (s *Store) Webhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id, w.url, array(SELECT e.kind FROM webhook_events e WHERE e.webhook_id = w.id), w.created_at
		FROM webhooks w ORDER BY w.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Webhook, error) {
		w, err := pgx.RowToStructByPos[Webhook](r)
		if w.Events == nil {
			w.Events = []domain.EventKind{}
		}
		slices.Sort(w.Events)
		return w, err
	})
}

// RemoveWebhook forgets a webhook and what was waiting to be sent to it.
func (s *Store) RemoveWebhook(ctx context.Context, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM webhooks WHERE id = $1`, id))
}

// QueueWebhooks queues the body made by body to every webhook that asked for kind, a job each.
// The body is made only where one did, as making it costs a query and most servers have none.
func (s *Store) QueueWebhooks(ctx context.Context, kind domain.EventKind, body func(context.Context) ([]byte, error)) error {
	var asked bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM webhook_events WHERE kind = $1)`, kind).Scan(&asked); err != nil || !asked {
		return err
	}
	b, err := body(ctx)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		WITH d AS (
			INSERT INTO webhook_deliveries (webhook_id, kind, body)
			SELECT webhook_id, kind, $2 FROM webhook_events WHERE kind = $1
			RETURNING id)
		INSERT INTO jobs (kind, subject) SELECT 'deliver_webhook', id FROM d`, kind, string(b))
	return err
}

// QueueDelivery queues a body to be sent to one webhook, whatever it asked for. ErrNotFound for
// no such webhook.
func (s *Store) QueueDelivery(ctx context.Context, webhook uuid.UUID, kind domain.EventKind, body []byte) error {
	return affected(s.pool.Exec(ctx, `
		WITH d AS (
			INSERT INTO webhook_deliveries (webhook_id, kind, body)
			SELECT id, $2, $3 FROM webhooks WHERE id = $1
			RETURNING id)
		INSERT INTO jobs (kind, subject) SELECT 'deliver_webhook', id FROM d`, webhook, kind, string(body)))
}

// Delivery is a body waiting to be sent, where to, and the secret it is signed with.
type Delivery struct {
	URL    string
	Secret string
	Kind   domain.EventKind
	Body   []byte
}

// Delivery answers a delivery waiting to be sent. ErrNotFound once it has been, or its webhook was
// removed.
func (s *Store) Delivery(ctx context.Context, id uuid.UUID) (Delivery, error) {
	var d Delivery
	var kind, body string
	err := s.pool.QueryRow(ctx, `
		SELECT w.url, w.secret, d.kind, d.body FROM webhook_deliveries d JOIN webhooks w ON w.id = d.webhook_id
		WHERE d.id = $1`, id).Scan(&d.URL, &d.Secret, &kind, &body)
	d.Kind, d.Body = domain.EventKind(kind), []byte(body)
	return d, found(err)
}

// Delivered forgets a delivery that has been sent.
func (s *Store) Delivered(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM webhook_deliveries WHERE id = $1`, id)
	return err
}

// Described is what a webhook is told of an event's profile, title and library, where it is
// about one that is still there.
type Described struct {
	ProfileName *string
	Title       *string
	TitleKind   *domain.ItemKind
	Year        *int
	LibraryName *string
}

func (s *Store) Describe(ctx context.Context, e domain.Event) (Described, error) {
	var d Described
	var kind *string
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT name FROM profiles WHERE id = $1), i.title, i.kind, i.year,
			(SELECT name FROM libraries WHERE id = $3)
		FROM (SELECT 1) one LEFT JOIN items i ON i.id = $2`,
		e.Profile, e.Item, e.Library).Scan(&d.ProfileName, &d.Title, &kind, &d.Year, &d.LibraryName)
	if kind != nil {
		d.TitleKind = new(domain.ItemKind(*kind))
	}
	return d, err
}
