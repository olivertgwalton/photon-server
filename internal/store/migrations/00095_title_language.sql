-- +goose Up
-- Which title a library gives its films and shows, as Plex's "Use original titles": localized, in
-- its language, by default, as before; or original, in the title's own.
ALTER TABLE libraries ADD COLUMN title_language text NOT NULL DEFAULT 'localized'
  CONSTRAINT title_language CHECK (title_language IN ('localized', 'original'));

-- +goose Down
ALTER TABLE libraries DROP COLUMN title_language;
