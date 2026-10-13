-- +goose Up
-- A library may hold films and shows together, and ranks the sources of each as its kind of
-- library would.
ALTER TABLE libraries
  DROP CONSTRAINT library_kind,
  ADD CONSTRAINT library_kind CHECK (kind IN ('movies', 'shows', 'mixed'));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ranked_as(library_kind text, item_kind text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN
  CASE
    WHEN library_kind IN ('shows', 'mixed') AND item_kind IN ('show', 'season', 'episode') THEN item_kind
    WHEN library_kind = 'shows' THEN 'show'
    ELSE 'movie'
  END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ranked_as(library_kind text, item_kind text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN
  CASE
    WHEN library_kind = 'shows' AND item_kind IN ('show', 'season', 'episode') THEN item_kind
    WHEN library_kind = 'shows' THEN 'show'
    ELSE 'movie'
  END;
-- +goose StatementEnd

ALTER TABLE libraries
  DROP CONSTRAINT library_kind,
  ADD CONSTRAINT library_kind CHECK (kind IN ('movies', 'shows'));
