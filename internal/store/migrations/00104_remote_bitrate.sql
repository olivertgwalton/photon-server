-- +goose Up
-- The most a stream to a client outside the server's own networks is sent at, as Jellyfin's
-- Internet streaming bitrate limit; 0 is none.
ALTER TABLE server ADD COLUMN remote_max_bitrate_kbps integer NOT NULL DEFAULT 0
  CONSTRAINT remote_max_bitrate_kbps CHECK (remote_max_bitrate_kbps >= 0);

-- +goose Down
ALTER TABLE server DROP COLUMN remote_max_bitrate_kbps;
