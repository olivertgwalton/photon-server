-- +goose Up
-- A remote library may hold what a search finds as well as, or instead of, a list's titles: the
-- titles a provider that searches finds are kept a day as discoveries, by a fixed id, until one is
-- opened and becomes a title of the library under it.
ALTER TABLE libraries ADD COLUMN discover_source text,
  DROP CONSTRAINT library_holding,
  ADD CONSTRAINT library_holding CHECK (CASE media
    WHEN 'folder' THEN root IS NOT NULL AND list_source IS NULL AND list_id IS NULL AND stream_source IS NULL
      AND discover_source IS NULL
    ELSE root IS NULL AND (list_source IS NULL) = (list_id IS NULL) AND stream_source IS NOT NULL
      AND (list_source IS NOT NULL OR discover_source IS NOT NULL) END);

CREATE TABLE discoveries (
  id uuid PRIMARY KEY,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  kind text NOT NULL CONSTRAINT discovery_kind CHECK (kind IN ('movie', 'show')),
  provider text NOT NULL
    CONSTRAINT discovery_provider CHECK (provider IN ('tmdb', 'imdb', 'tvdb') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  value text NOT NULL,
  title text NOT NULL,
  year int,
  overview text,
  poster_id uuid NOT NULL,
  poster_url text,
  found_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX discoveries_poster ON discoveries (poster_id);
CREATE INDEX discoveries_library ON discoveries (library_id);

-- +goose Down
DROP TABLE discoveries;
DELETE FROM libraries WHERE media = 'remote' AND list_source IS NULL;
ALTER TABLE libraries DROP CONSTRAINT library_holding, DROP COLUMN discover_source,
  ADD CONSTRAINT library_holding CHECK (CASE media
    WHEN 'folder' THEN root IS NOT NULL AND list_source IS NULL AND list_id IS NULL AND stream_source IS NULL
    ELSE root IS NULL AND list_source IS NOT NULL AND list_id IS NOT NULL AND stream_source IS NOT NULL END);
