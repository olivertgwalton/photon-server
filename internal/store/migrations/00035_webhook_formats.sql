-- +goose Up
-- A webhook is sent its events as photon's own signed JSON, or as a chat or push service takes a
-- message.
ALTER TABLE webhooks ADD COLUMN format text NOT NULL DEFAULT 'photon'
  CONSTRAINT webhook_format CHECK (format IN ('photon', 'discord', 'slack', 'ntfy', 'gotify'));

-- +goose Down
ALTER TABLE webhooks DROP COLUMN format;
