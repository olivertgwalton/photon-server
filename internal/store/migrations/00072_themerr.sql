-- +goose Up
-- Themes are no longer taken from Plex's theme host. The online source is now ThemerrDB's YouTube
-- links, opt-in as Jellyfin's Themerr plugin is, so every library keeps to its files until asked.
ALTER TABLE libraries DROP CONSTRAINT theme_lookup, ALTER COLUMN themes SET DEFAULT 'local';
UPDATE libraries SET themes = 'local' WHERE themes = 'all';
ALTER TABLE libraries ADD CONSTRAINT theme_lookup CHECK (themes IN ('local', 'themerr', 'off'));

DELETE FROM themes WHERE source = 'tvthemes';
ALTER TABLE themes DROP CONSTRAINT theme_source,
  ADD CONSTRAINT theme_source CHECK (source IN ('file', 'themerr'));

-- +goose Down
DELETE FROM themes WHERE source = 'themerr';
ALTER TABLE themes DROP CONSTRAINT theme_source,
  ADD CONSTRAINT theme_source CHECK (source IN ('file', 'tvthemes'));

ALTER TABLE libraries DROP CONSTRAINT theme_lookup, ALTER COLUMN themes SET DEFAULT 'all';
UPDATE libraries SET themes = 'all' WHERE themes = 'themerr';
ALTER TABLE libraries ADD CONSTRAINT theme_lookup CHECK (themes IN ('all', 'local', 'off'));
