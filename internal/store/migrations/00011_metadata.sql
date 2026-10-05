-- +goose Up
-- scan_title is what the scanner read from the files, used only to find the title again; title is
-- the best value any source has given, chosen by item_fields.
ALTER TABLE items
  ADD COLUMN scan_title text,
  ADD COLUMN original_title text,
  ADD COLUMN overview text,
  ADD COLUMN tagline text,
  ADD COLUMN certificate text,
  ADD COLUMN release_date date,
  ADD COLUMN genres jsonb,
  ADD COLUMN studios jsonb;
UPDATE items SET scan_title = title;
ALTER TABLE items ALTER COLUMN scan_title SET NOT NULL;

CREATE TABLE item_fields (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  field text NOT NULL CONSTRAINT field CHECK (field IN ('title', 'sort_title', 'original_title',
    'overview', 'tagline', 'certificate', 'release_date', 'year', 'genres', 'studios')),
  source text NOT NULL CONSTRAINT field_source CHECK (source IN ('file', 'tmdb', 'nfo', 'user')),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (item_id, field)
);
INSERT INTO item_fields (item_id, field, source)
  SELECT id, f, 'file' FROM items, unnest(ARRAY['title', 'sort_title']) f;
INSERT INTO item_fields (item_id, field, source) SELECT id, 'year', 'file' FROM items WHERE year IS NOT NULL;

-- +goose Down
DROP TABLE item_fields;
ALTER TABLE items DROP COLUMN scan_title, DROP COLUMN original_title, DROP COLUMN overview,
  DROP COLUMN tagline, DROP COLUMN certificate, DROP COLUMN release_date, DROP COLUMN genres,
  DROP COLUMN studios;
