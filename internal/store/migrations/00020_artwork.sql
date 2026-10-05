-- +goose Up
-- A title's pictures: files in its library (folder and rel_path) or a provider's (url).
CREATE TABLE artwork (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL CONSTRAINT artwork_source CHECK (source IN ('file', 'tmdb', 'tvdb')),
  kind text NOT NULL CONSTRAINT artwork_kind CHECK (kind IN ('poster', 'backdrop', 'logo', 'thumb', 'banner')),
  place text NOT NULL,
  position smallint NOT NULL,
  folder text,
  language text,
  width int,
  height int,
  PRIMARY KEY (item_id, source, kind, place),
  CONSTRAINT artwork_file_has_folder CHECK ((source = 'file') = (folder IS NOT NULL))
);

-- +goose Down
DROP TABLE artwork;
