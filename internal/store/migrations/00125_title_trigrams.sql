-- +goose NO TRANSACTION
-- +goose Up
-- A title typed with a letter wrong is found by the trigrams it shares with what was typed, once
-- none has the words typed; the index is on the same text the search column is made of.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_trigrams ON items
  USING gin (search_text(title || ' ' || coalesce(original_title, '')) gin_trgm_ops);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS items_trigrams;
DROP EXTENSION IF EXISTS pg_trgm;
