-- +goose Up
-- Which of a title's pictures a library takes first, as Plex's "Prefer artwork based on library
-- language": localized, those in its language, by default, as before; or any, the most liked.
ALTER TABLE libraries ADD COLUMN artwork_language text NOT NULL DEFAULT 'localized'
  CONSTRAINT artwork_language CHECK (artwork_language IN ('localized', 'any'));

-- +goose Down
ALTER TABLE libraries DROP COLUMN artwork_language;
