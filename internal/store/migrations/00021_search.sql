-- +goose Up
CREATE EXTENSION IF NOT EXISTS unaccent;
-- unaccent is only stable, as its rules could change; a title's search words are stored, so this
-- fixes them to the rules the extension has now. A new extension version means running
-- UPDATE items SET title = title.
CREATE FUNCTION search_text(t text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
  RETURN lower(public.unaccent('public.unaccent'::regdictionary, t));
ALTER TABLE items ADD COLUMN search tsvector NOT NULL GENERATED ALWAYS AS
  (to_tsvector('simple', search_text(title || ' ' || coalesce(original_title, '')))) STORED;
CREATE INDEX items_search ON items USING gin (search);

-- +goose Down
ALTER TABLE items DROP COLUMN search;
DROP FUNCTION search_text(text);
