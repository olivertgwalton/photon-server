-- +goose Up
-- When each node last said it was up, so one that stops answering is shown as when it was last seen.
ALTER TABLE node ADD COLUMN last_seen timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE node DROP COLUMN last_seen;
