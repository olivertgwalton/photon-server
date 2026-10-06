-- +goose NO TRANSACTION
-- +goose Up
-- A wall narrowed to genres or studios finds its titles by ?| on these rather than reading the
-- whole library.
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_genres ON items USING gin (genres);
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_studios ON items USING gin (studios);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS items_studios;
DROP INDEX CONCURRENTLY IF EXISTS items_genres;
