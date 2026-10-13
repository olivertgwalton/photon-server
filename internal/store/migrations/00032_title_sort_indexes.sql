-- +goose NO TRANSACTION
-- +goose Up
-- A wall of titles is read along these in its sort; a wall of episodes never is, sorting them by
-- their show's title and by the day each aired. Kept of every item, they were mostly episodes:
-- 207 MB of a 580k-item library's, against 16 MB of its titles alone.
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_titles_sort ON items (library_id, kind, sort_title, id)
  WHERE kind <> 'episode';
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_titles_released_asc ON items (library_id, kind, released_asc, id)
  WHERE kind <> 'episode';
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_titles_released_desc ON items (library_id, kind, released_desc, id)
  WHERE kind <> 'episode';
DROP INDEX CONCURRENTLY IF EXISTS items_library_sort;
DROP INDEX CONCURRENTLY IF EXISTS items_library_released_asc;
DROP INDEX CONCURRENTLY IF EXISTS items_library_released_desc;

-- +goose Down
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_library_sort ON items (library_id, kind, sort_title, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_library_released_asc ON items (library_id, kind, released_asc, id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_library_released_desc ON items (library_id, kind, released_desc, id);
DROP INDEX CONCURRENTLY IF EXISTS items_titles_sort;
DROP INDEX CONCURRENTLY IF EXISTS items_titles_released_asc;
DROP INDEX CONCURRENTLY IF EXISTS items_titles_released_desc;
