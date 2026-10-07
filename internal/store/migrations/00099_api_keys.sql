-- +goose Up
-- A session is a device's, signed in, or an API key an admin made, as Jellyfin's are: a key is for
-- a script or another server, so it never lapses and has no expiry.
ALTER TABLE device_sessions
  ADD COLUMN kind text NOT NULL DEFAULT 'device' CONSTRAINT session_kind CHECK (kind IN ('device', 'key')),
  ALTER COLUMN expires_at DROP NOT NULL,
  ADD CONSTRAINT key_never_lapses CHECK ((kind = 'key') = (expires_at IS NULL));

-- +goose Down
DELETE FROM device_sessions WHERE kind = 'key';
ALTER TABLE device_sessions DROP CONSTRAINT key_never_lapses, DROP COLUMN kind,
  ALTER COLUMN expires_at SET NOT NULL;
