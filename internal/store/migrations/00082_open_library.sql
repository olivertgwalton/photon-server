-- +goose Up
-- A books library asks Open Library of its authors and books, as one added now does. The table is
-- small, a few rows a library.
INSERT INTO library_sources (library_id, item_kind, fetcher, source, position, enabled)
SELECT l.id, k.item_kind, f.fetcher, 'openlibrary', 0, true FROM libraries l,
  (VALUES ('author'), ('book')) AS k (item_kind), (VALUES ('metadata'), ('images')) AS f (fetcher)
WHERE l.kind = 'books'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM library_sources WHERE source = 'openlibrary';
