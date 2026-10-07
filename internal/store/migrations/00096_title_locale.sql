-- +goose NO TRANSACTION
-- +goose Up
-- A film's or show's own metadata language and certification country, over its library's, as
-- Jellyfin's item settings: each its library's while unset. Nullable with no default, so adding
-- them rewrites nothing; the check is validated apart, reading the table without stopping writes.
ALTER TABLE items ADD COLUMN IF NOT EXISTS metadata_language text, ADD COLUMN IF NOT EXISTS certification_country text;
ALTER TABLE items DROP CONSTRAINT IF EXISTS item_certification_country,
  ADD CONSTRAINT item_certification_country CHECK (certification_country ~ '^[A-Z]{2}$') NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT item_certification_country;

-- +goose Down
ALTER TABLE items DROP COLUMN IF EXISTS metadata_language, DROP COLUMN IF EXISTS certification_country;
