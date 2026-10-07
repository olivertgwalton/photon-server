-- +goose Up
-- When an admin took a film or show off its providers: held so, it is matched to none until its
-- match is fixed or it is refreshed itself. Nullable with no default, so adding it rewrites nothing.
ALTER TABLE items ADD COLUMN unmatched_at timestamptz;

-- +goose Down
ALTER TABLE items DROP COLUMN unmatched_at;
