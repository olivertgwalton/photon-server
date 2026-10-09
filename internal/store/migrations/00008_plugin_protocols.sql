-- +goose Up
-- A plugin speaks photon's own protocol, or is a Stremio addon, registered by its manifest.
ALTER TABLE plugins ADD COLUMN protocol text NOT NULL DEFAULT 'photon'
  CONSTRAINT plugin_protocol CHECK (protocol IN ('photon', 'stremio'));

-- +goose Down
DELETE FROM plugins WHERE protocol = 'stremio';
ALTER TABLE plugins DROP COLUMN protocol;
