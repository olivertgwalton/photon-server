-- +goose Up
-- A version an admin merged into another film: a scan of its folder finds that film by it, and
-- leaves the film's own name and description alone. A column with no default is added without
-- rewriting the table.
ALTER TABLE versions ADD COLUMN merged_at timestamptz;

-- +goose Down
ALTER TABLE versions DROP COLUMN merged_at;
