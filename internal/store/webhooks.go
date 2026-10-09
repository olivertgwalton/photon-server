package store

import (
	"context"
	"crypto/rand"
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
		FROM webhooks w WHERE w.plugin IS NULL ORDER BY w.id`)
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

// RemoveWebhook forgets a webhook and what was waiting to be sent to it. A plugin's goes only with
// the plugin.
func (s *Store) RemoveWebhook(ctx context.Context, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM webhooks WHERE id = $1 AND plugin IS NULL`, id))
}

// Hearing is the events a plugin hears and where it is told of them; none is a plugin told of none.
type Hearing struct {
	URL   string
	Kinds []domain.EventKind
}

// hear makes a plugin's webhook what it hears now, keeping what is waiting to be sent to it.
func hear(ctx context.Context, tx pgx.Tx, slug string, h Hearing) error {
	if len(h.Kinds) == 0 {
		_, err := tx.Exec(ctx, `DELETE FROM webhooks WHERE plugin = $1`, slug)
		return err
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO webhooks (url, secret, plugin) VALUES ($1, $2, $3)
		ON CONFLICT (plugin) DO UPDATE SET url = excluded.url RETURNING id`, h.URL, rand.Text(), slug).Scan(&id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM webhook_events WHERE webhook_id = $1`, id); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO webhook_events (webhook_id, kind) SELECT $1, unnest($2::text[])`, id, h.Kinds)
	return err
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
			SELECT id, $2, $3 FROM webhooks WHERE id = $1 AND plugin IS NULL
			RETURNING id)
		INSERT INTO jobs (kind, subject) SELECT 'deliver_webhook', id FROM d`, webhook, kind, string(body)))
}

// Delivery is a body waiting to be sent, where to, and the secret it is signed with; and the
// plugin it is to, where it is to one.
type Delivery struct {
	URL    string
	Secret string
	Kind   domain.EventKind
	Body   []byte
	Plugin *string
}

// Delivery answers a delivery waiting to be sent. ErrNotFound once it has been, or its webhook was
// removed.
func (s *Store) Delivery(ctx context.Context, id uuid.UUID) (Delivery, error) {
	var d Delivery
	err := s.pool.QueryRow(ctx, `
		SELECT w.url, w.secret, d.kind, d.body, w.plugin FROM webhook_deliveries d JOIN webhooks w ON w.id = d.webhook_id
		WHERE d.id = $1`, id).Scan(&d.URL, &d.Secret, &d.Kind, &d.Body, &d.Plugin)
	return d, found(err)
}

// Delivered forgets a delivery that has been sent.
func (s *Store) Delivered(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM webhook_deliveries WHERE id = $1`, id)
	return err
}

// Described is what a webhook is told of an event's profile, title and library, where it is
// about one that is still there: of an episode, its numbers and its show too.
type Described struct {
	ProfileName *string
	Title       *string
	TitleKind   *domain.ItemKind
	Year        *int
	TitleIDs    map[domain.Provider]string
	Season      *int
	Episode     *int
	Show        *uuid.UUID
	ShowTitle   *string
	ShowYear    *int
	ShowIDs     map[domain.Provider]string
	LibraryName *string
}

func (s *Store) Describe(ctx context.Context, e domain.Event) (Described, error) {
	var d Described
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT name FROM profiles WHERE id = $1), i.title, i.kind, i.year, `+knownIDs("i")+`,
			i.season_number, i.episode_number, show.id, show.title, show.year, `+knownIDs("show")+`,
			(SELECT name FROM libraries WHERE id = $3)
		FROM (SELECT 1) one LEFT JOIN items i ON i.id = $2
		LEFT JOIN items season ON season.id = i.parent_id AND i.kind = 'episode'
		LEFT JOIN items show ON show.id = season.parent_id`,
		e.Profile, e.Item, e.Library).Scan(&d.ProfileName, &d.Title, &d.TitleKind, &d.Year, &d.TitleIDs,
		&d.Season, &d.Episode, &d.Show, &d.ShowTitle, &d.ShowYear, &d.ShowIDs, &d.LibraryName)
	return d, err
}

// knownIDs selects the ids the built-in providers know an item by, as a JSON object.
func knownIDs(item string) string {
	return `(SELECT jsonb_object_agg(provider, value) FROM external_ids WHERE item_id = ` + item + `.id
		AND provider IN ('imdb', 'tmdb', 'tvdb'))`
}
