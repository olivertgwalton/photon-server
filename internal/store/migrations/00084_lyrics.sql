-- +goose NO TRANSACTION
-- +goose Up
-- A library ranks the sources it asks for a track's lyrics, as it ranks those it asks for metadata
-- and pictures; a music library asks LRCLIB, as one added now does. What a source answered of a
-- track is kept, none included, so it is asked once. The check is checked again apart from its
-- change, reading the table without stopping its writes.
ALTER TABLE library_sources DROP CONSTRAINT library_source_fetcher,
  ADD CONSTRAINT library_source_fetcher CHECK (fetcher IN ('metadata', 'images', 'lyrics')) NOT VALID;
ALTER TABLE library_sources VALIDATE CONSTRAINT library_source_fetcher;
INSERT INTO library_sources (library_id, item_kind, fetcher, source, position, enabled)
SELECT id, 'track', 'lyrics', 'lrclib', 0, true FROM libraries WHERE kind = 'music'
ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS fetched_lyrics (
  item_id uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
  source text NOT NULL,
  -- NULL is a source that had none.
  lyrics text,
  fetched_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (item_id, source)
);

-- +goose Down
DROP TABLE IF EXISTS fetched_lyrics;
DELETE FROM library_sources WHERE fetcher = 'lyrics';
ALTER TABLE library_sources DROP CONSTRAINT library_source_fetcher,
  ADD CONSTRAINT library_source_fetcher CHECK (fetcher IN ('metadata', 'images')) NOT VALID;
ALTER TABLE library_sources VALIDATE CONSTRAINT library_source_fetcher;
