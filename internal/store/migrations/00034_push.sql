-- +goose NO TRANSACTION
-- +goose Up
-- A browser subscribed for a profile is pushed its notifications as they come, by a job for each
-- profile, sent with the server's VAPID key, which the first node to need it makes. Each check
-- is checked again apart from its change, reading the table without stopping its writes.
ALTER TABLE server ADD COLUMN push_key bytea;

CREATE TABLE push_subscriptions (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  endpoint text NOT NULL CONSTRAINT push_subscriptions_endpoint_key UNIQUE,
  p256dh bytea NOT NULL,
  auth bytea NOT NULL,
  language text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX push_subscriptions_profile ON push_subscriptions (profile_id);

ALTER TABLE notification_items ADD COLUMN pushed boolean NOT NULL DEFAULT true;
CREATE INDEX CONCURRENTLY IF NOT EXISTS notification_items_unpushed ON notification_items (profile_id) WHERE NOT pushed;

ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history', 'segments', 'push')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;

-- +goose Down
DELETE FROM jobs WHERE kind = 'push';
ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history', 'segments'));
DROP INDEX CONCURRENTLY IF EXISTS notification_items_unpushed;
ALTER TABLE notification_items DROP COLUMN pushed;
DROP TABLE push_subscriptions;
ALTER TABLE server DROP COLUMN push_key;
