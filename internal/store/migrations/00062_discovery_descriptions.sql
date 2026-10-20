-- +goose Up
-- A title a search found is described, as a title's page draws it, the first time its page is
-- opened, and keeps the pictures that description names, under ids of their addresses, for as
-- long as the find is kept.
ALTER TABLE discoveries ADD COLUMN description jsonb;
CREATE TABLE discovery_pictures (
  id uuid PRIMARY KEY,
  discovery_id uuid NOT NULL REFERENCES discoveries(id) ON DELETE CASCADE,
  url text NOT NULL
);
CREATE INDEX discovery_pictures_discovery ON discovery_pictures (discovery_id);

-- +goose Down
DROP TABLE discovery_pictures;
ALTER TABLE discoveries DROP COLUMN description;
