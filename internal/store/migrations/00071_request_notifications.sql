-- +goose NO TRANSACTION
-- +goose Up
-- A notification tells of a title, or of a request that waits for the profile to decide it: each
-- who may decide a request is told of it once, and the notification goes with the request. Each
-- check is checked again apart from its change, reading the table without stopping its writes.
ALTER TABLE notifications
  ADD COLUMN kind text NOT NULL DEFAULT 'title',
  ADD COLUMN request_id uuid,
  ALTER COLUMN title_id DROP NOT NULL;
ALTER TABLE notifications ADD CONSTRAINT notification_kind CHECK (kind IN ('title', 'request')) NOT VALID;
ALTER TABLE notifications VALIDATE CONSTRAINT notification_kind;
ALTER TABLE notifications ADD CONSTRAINT notification_subject CHECK (CASE kind
  WHEN 'title' THEN title_id IS NOT NULL AND request_id IS NULL
  ELSE request_id IS NOT NULL AND title_id IS NULL END) NOT VALID;
ALTER TABLE notifications VALIDATE CONSTRAINT notification_subject;
ALTER TABLE notifications ADD CONSTRAINT notification_request
  FOREIGN KEY (request_id) REFERENCES requests(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE notifications VALIDATE CONSTRAINT notification_request;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS notifications_request ON notifications (request_id, profile_id)
  WHERE request_id IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS notifications_request;
DELETE FROM notifications WHERE kind = 'request';
ALTER TABLE notifications DROP CONSTRAINT notification_request, DROP CONSTRAINT notification_subject,
  DROP CONSTRAINT notification_kind, DROP COLUMN request_id, DROP COLUMN kind,
  ALTER COLUMN title_id SET NOT NULL;
