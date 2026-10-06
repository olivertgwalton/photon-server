-- +goose NO TRANSACTION
-- +goose Up
-- The newest titles of a kind across every library, for home's recently added rows, read from the
-- top rather than sorting every film.
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_added ON items (kind, added_at DESC, id DESC);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS items_added;
