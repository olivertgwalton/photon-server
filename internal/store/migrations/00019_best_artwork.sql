-- +goose NO TRANSACTION
-- +goose Up
-- A card shows each title's best picture of each kind. Ranking all of a title's pictures as each
-- card was drawn was most of the time a wall took, so the best are kept: by triggers as a title's
-- pictures change, and by the store as a library's picture sources do. Each statement is its own
-- transaction, so a trigger locks artwork only while it is made, beside the server still running.
CREATE TABLE best_artwork (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  kind text NOT NULL,
  artwork_id uuid NOT NULL,
  blurhash text,
  PRIMARY KEY (item_id, kind)
);

-- The titles are locked first, so two transactions changing one title's pictures choose its best
-- in turn, the second seeing what the first saved.
-- +goose StatementBegin
CREATE FUNCTION keep_best_artwork(titles uuid[]) RETURNS void LANGUAGE sql AS $$
  SELECT FROM items WHERE id = ANY(titles) ORDER BY id FOR NO KEY UPDATE;
  DELETE FROM best_artwork WHERE item_id = ANY(titles);
  INSERT INTO best_artwork (item_id, kind, artwork_id, blurhash)
  SELECT DISTINCT ON (item_id, kind) item_id, kind, id, blurhash FROM ranked_artwork(titles)
  ORDER BY item_id, kind, rank, "position", id;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION artwork_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    PERFORM keep_best_artwork(ARRAY(SELECT DISTINCT item_id FROM added));
  ELSIF TG_OP = 'DELETE' THEN
    PERFORM keep_best_artwork(ARRAY(SELECT DISTINCT item_id FROM gone));
  ELSE
    PERFORM keep_best_artwork(ARRAY(SELECT item_id FROM gone UNION SELECT item_id FROM added));
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER artwork_added AFTER INSERT ON artwork REFERENCING NEW TABLE AS added
  FOR EACH STATEMENT EXECUTE FUNCTION artwork_changed();
CREATE TRIGGER artwork_gone AFTER DELETE ON artwork REFERENCING OLD TABLE AS gone
  FOR EACH STATEMENT EXECUTE FUNCTION artwork_changed();
CREATE TRIGGER artwork_updated AFTER UPDATE ON artwork REFERENCING OLD TABLE AS gone NEW TABLE AS added
  FOR EACH STATEMENT EXECUTE FUNCTION artwork_changed();

-- The titles with pictures are chosen for a thousand at a time, each thousand committed as it is
-- done, so the titles are locked only that long beside the server still saving them.
-- +goose StatementBegin
DO $$
DECLARE
  titles uuid[];
  last uuid := '00000000-0000-0000-0000-000000000000';
BEGIN
  LOOP
    SELECT array_agg(item_id ORDER BY item_id) INTO titles
    FROM (SELECT DISTINCT item_id FROM artwork WHERE item_id > last ORDER BY item_id LIMIT 1000) t;
    EXIT WHEN titles IS NULL;
    PERFORM keep_best_artwork(titles);
    last := titles[cardinality(titles)];
    COMMIT;
  END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER artwork_updated ON artwork;
DROP TRIGGER artwork_gone ON artwork;
DROP TRIGGER artwork_added ON artwork;
DROP FUNCTION artwork_changed();
DROP FUNCTION keep_best_artwork(uuid[]);
DROP TABLE best_artwork;
