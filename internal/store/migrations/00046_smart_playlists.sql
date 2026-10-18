-- +goose NO TRANSACTION
-- +goose Up
-- A smart playlist is a rule over one library's wall, as Plex's smart playlists are. Its entries
-- are what the rule finds for the profile whose playlist it is, read as the playlist is read, so
-- they follow that profile's own marks and plays. One made by hand holds entries instead, and has
-- neither a rule nor a library. Each constraint is checked apart from its change, reading the
-- table without stopping its writes.
ALTER TABLE playlists ADD COLUMN library_id uuid, ADD COLUMN rule jsonb;
ALTER TABLE playlists ADD CONSTRAINT playlist_library FOREIGN KEY (library_id)
  REFERENCES libraries(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE playlists VALIDATE CONSTRAINT playlist_library;
ALTER TABLE playlists ADD CONSTRAINT playlist_rule
  CHECK ((rule IS NULL) = (library_id IS NULL)) NOT VALID;
ALTER TABLE playlists VALIDATE CONSTRAINT playlist_rule;
CREATE INDEX CONCURRENTLY IF NOT EXISTS playlists_library ON playlists (library_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS playlists_library;
ALTER TABLE playlists DROP CONSTRAINT playlist_rule, DROP CONSTRAINT playlist_library,
  DROP COLUMN rule, DROP COLUMN library_id;
