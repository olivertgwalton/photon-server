-- +goose Up
-- Addresses told of events as they happen, as Plex's webhooks are. The secret signs each body; it
-- is answered once, when the webhook is made.
CREATE TABLE webhooks (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  url text NOT NULL,
  secret text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE webhook_events (
  webhook_id uuid NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  kind text NOT NULL CONSTRAINT webhook_event CHECK (kind IN (
    'playback.started', 'playback.paused', 'playback.resumed', 'playback.stopped',
    'auth.signed_in', 'auth.sign_in_refused', 'profile.added', 'profile.removed',
    'library.added', 'library.removed', 'library.scanned', 'library.titles_added',
    'task.failed', 'backup.made')),
  PRIMARY KEY (webhook_id, kind)
);
CREATE INDEX webhook_events_kind ON webhook_events (kind);

-- A body waiting to be sent, as it will be signed; its job is deliver_webhook, the delivery its
-- subject.
CREATE TABLE webhook_deliveries (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  webhook_id uuid NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  kind text NOT NULL CONSTRAINT delivery_event CHECK (kind IN (
    'playback.started', 'playback.paused', 'playback.resumed', 'playback.stopped',
    'auth.signed_in', 'auth.sign_in_refused', 'profile.added', 'profile.removed',
    'library.added', 'library.removed', 'library.scanned', 'library.titles_added',
    'task.failed', 'backup.made', 'webhook.test')),
  body text NOT NULL
);

ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook'));

-- +goose Down
DELETE FROM jobs WHERE kind = 'deliver_webhook';
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library', 'markers', 'previews', 'convert'));
DROP TABLE webhook_deliveries, webhook_events, webhooks;
