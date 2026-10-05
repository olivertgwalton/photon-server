-- +goose Up
-- A collection is an item of its own, with its own pictures and description: a box set a provider
-- names its titles part of, or one an admin made. Its titles are members, not children, as a
-- title may be in several.
ALTER TABLE items DROP CONSTRAINT item_kind,
  ADD CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season', 'episode', 'extra', 'collection'));

CREATE TABLE collections (
  item_id uuid PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  origin text NOT NULL CONSTRAINT collection_origin CHECK (origin IN ('tmdb', 'user'))
);

CREATE TABLE collection_members (
  collection_id uuid NOT NULL REFERENCES collections(item_id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  position int NOT NULL DEFAULT 0,
  PRIMARY KEY (collection_id, item_id)
);
CREATE INDEX collection_members_item ON collection_members (item_id);

-- +goose Down
DROP TABLE collection_members, collections;
DELETE FROM items WHERE kind = 'collection';
ALTER TABLE items DROP CONSTRAINT item_kind,
  ADD CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season', 'episode', 'extra'));
