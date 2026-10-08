-- +goose NO TRANSACTION
-- +goose Up
-- The same bytes may be a version of two titles, as of two episodes riven shows under each of
-- their numbers, though still one version of each.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS versions_library_fingerprint_item ON versions (library_id, fingerprint, item_id);
ALTER TABLE versions DROP CONSTRAINT IF EXISTS versions_library_id_fingerprint_key;

-- +goose Down
-- Refused while two titles hold the same bytes.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS versions_library_id_fingerprint_key ON versions (library_id, fingerprint);
ALTER TABLE versions ADD CONSTRAINT versions_library_id_fingerprint_key UNIQUE USING INDEX versions_library_id_fingerprint_key;
DROP INDEX CONCURRENTLY IF EXISTS versions_library_fingerprint_item;
