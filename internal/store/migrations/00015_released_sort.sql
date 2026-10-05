-- +goose Up
-- A title's release date, else the first of its year, as Jellyfin sorts; one with neither goes
-- last whichever way the wall reads, so each direction has its own never-null key to page by.
ALTER TABLE items
  ADD COLUMN released_asc date NOT NULL
    GENERATED ALWAYS AS (coalesce(release_date, make_date(year, 1, 1), '9999-12-31')) STORED,
  ADD COLUMN released_desc date NOT NULL
    GENERATED ALWAYS AS (coalesce(release_date, make_date(year, 1, 1), '0001-01-01')) STORED;
CREATE INDEX items_library_released_asc ON items (library_id, kind, released_asc, id);
CREATE INDEX items_library_released_desc ON items (library_id, kind, released_desc, id);
CREATE INDEX items_library_added ON items (library_id, kind, added_at, id);

-- +goose Down
DROP INDEX items_library_added;
ALTER TABLE items DROP COLUMN released_asc, DROP COLUMN released_desc;
