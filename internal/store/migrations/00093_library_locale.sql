-- +goose Up
-- The language a library's metadata is asked for in, an IETF tag such as en-GB, and the country
-- whose certificates, an ISO 3166-1 alpha-2 code such as GB, as Jellyfin's library options and
-- Plex's certification country: each the server's own while unset.
ALTER TABLE libraries ADD COLUMN metadata_language text, ADD COLUMN certification_country text
  CONSTRAINT certification_country CHECK (certification_country ~ '^[A-Z]{2}$');

-- +goose Down
ALTER TABLE libraries DROP COLUMN metadata_language, DROP COLUMN certification_country;
