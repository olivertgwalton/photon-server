-- +goose Up
-- When an admin split a copy off its title onto one of its own: a scan leaves it there, where it
-- would otherwise group a folder's copies onto one title again. Nullable with no default, so adding
-- it rewrites nothing.
ALTER TABLE versions ADD COLUMN split_at timestamptz;

-- +goose Down
ALTER TABLE versions DROP COLUMN split_at;
