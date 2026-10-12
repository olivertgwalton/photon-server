-- +goose Up
-- A collection with no poster of its own is drawn as a mosaic of its members' posters, as Plex's
-- are: one for each set of them a viewer may see, so a limited profile is never shown a poster it
-- may not see. Its id is made from its posters, and what a collection holds changing forgets its
-- mosaics, which the picture cache then sweeps away.
CREATE TABLE mosaics (
  id uuid PRIMARY KEY,
  collection_id uuid NOT NULL REFERENCES collections(item_id) ON DELETE CASCADE,
  parts uuid[] NOT NULL CONSTRAINT mosaic_parts CHECK (cardinality(parts) BETWEEN 2 AND 4),
  blurhash text
);
CREATE INDEX mosaics_collection ON mosaics (collection_id);

-- +goose StatementBegin
CREATE FUNCTION forget_mosaics() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  DELETE FROM mosaics WHERE collection_id IN (SELECT collection_id FROM changed);
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER mosaics_added AFTER INSERT ON collection_members
  REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION forget_mosaics();
CREATE TRIGGER mosaics_moved AFTER UPDATE ON collection_members
  REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION forget_mosaics();
CREATE TRIGGER mosaics_removed AFTER DELETE ON collection_members
  REFERENCING OLD TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION forget_mosaics();

-- +goose Down
DROP TRIGGER mosaics_removed ON collection_members;
DROP TRIGGER mosaics_moved ON collection_members;
DROP TRIGGER mosaics_added ON collection_members;
DROP FUNCTION forget_mosaics();
DROP TABLE mosaics;
