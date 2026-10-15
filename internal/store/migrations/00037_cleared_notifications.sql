-- +goose Up
-- A profile clears notifications from its inbox. A cleared one is kept, read, so what it told of
-- is still told once.
ALTER TABLE notifications ADD COLUMN cleared_at timestamptz;

-- +goose Down
ALTER TABLE notifications DROP COLUMN cleared_at;
