-- +goose NO TRANSACTION
-- +goose Up
-- The titles in a folder, which the scanner looks for whenever it meets a copy it does not know.
-- Without it each lookup read the whole library, so a first import grew as its square.
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_library_folder ON items (library_id, folder);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS items_library_folder;
