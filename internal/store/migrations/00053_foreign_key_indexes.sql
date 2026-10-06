-- +goose NO TRANSACTION
-- +goose Up
-- An index behind every foreign key that lacked one. Without them each title removed scanned its
-- plays, activity, copies and playlist entries whole, and every card read every copy to find its
-- length. Built concurrently, so a large library keeps playing while they are made.
CREATE INDEX CONCURRENTLY IF NOT EXISTS versions_item ON versions (item_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS watch_state_item ON watch_state (item_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS favourites_item ON favourites (item_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS playlist_entries_item ON playlist_entries (item_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS plays_item ON plays (item_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS plays_version ON plays (version_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS downloads_item ON downloads (item_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS downloads_part ON downloads (part_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS downloads_session ON downloads (session_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS activity_item ON activity (item_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS activity_profile ON activity (profile_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS activity_library ON activity (library_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS profile_libraries_library ON profile_libraries (library_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS webhook_deliveries_webhook ON webhook_deliveries (webhook_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS webhook_deliveries_webhook;
DROP INDEX CONCURRENTLY IF EXISTS profile_libraries_library;
DROP INDEX CONCURRENTLY IF EXISTS activity_library;
DROP INDEX CONCURRENTLY IF EXISTS activity_profile;
DROP INDEX CONCURRENTLY IF EXISTS activity_item;
DROP INDEX CONCURRENTLY IF EXISTS downloads_session;
DROP INDEX CONCURRENTLY IF EXISTS downloads_part;
DROP INDEX CONCURRENTLY IF EXISTS downloads_item;
DROP INDEX CONCURRENTLY IF EXISTS plays_version;
DROP INDEX CONCURRENTLY IF EXISTS plays_item;
DROP INDEX CONCURRENTLY IF EXISTS playlist_entries_item;
DROP INDEX CONCURRENTLY IF EXISTS favourites_item;
DROP INDEX CONCURRENTLY IF EXISTS watch_state_item;
DROP INDEX CONCURRENTLY IF EXISTS versions_item;
