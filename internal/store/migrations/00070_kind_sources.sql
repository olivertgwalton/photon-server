-- +goose Up
-- A library ranks its sources per kind of item it holds, for metadata and for pictures apart, as
-- Jellyfin's library options do; a source unticked keeps its place. Each library's one list is
-- carried into every kind it holds, less what a source cannot give of that kind.
ALTER TABLE library_sources DROP CONSTRAINT library_sources_pkey,
  DROP CONSTRAINT library_sources_library_id_position_key,
  ADD COLUMN item_kind text CONSTRAINT library_source_item_kind CHECK (item_kind IN ('movie', 'show', 'season', 'episode')),
  ADD COLUMN fetcher text CONSTRAINT library_source_fetcher CHECK (fetcher IN ('metadata', 'images')),
  ADD COLUMN enabled boolean NOT NULL DEFAULT true;
INSERT INTO library_sources (library_id, item_kind, fetcher, source, position)
  SELECT ls.library_id, k.kind, f.fetcher, ls.source,
    row_number() OVER (PARTITION BY ls.library_id, k.kind, f.fetcher ORDER BY ls.position) - 1
  FROM library_sources ls
  JOIN libraries l ON l.id = ls.library_id
  JOIN (VALUES ('movies', 'movie'), ('shows', 'show'), ('shows', 'season'), ('shows', 'episode')) k(library, kind)
    ON k.library = l.kind
  CROSS JOIN (VALUES ('metadata'), ('images')) f(fetcher)
  WHERE ls.item_kind IS NULL
    AND (ls.source LIKE 'plugin:%'
      OR (f.fetcher = 'metadata' AND ls.source IN ('nfo', 'tmdb'))
      OR (f.fetcher = 'images' AND ls.source = 'tmdb')
      OR (ls.source = 'tvdb' AND k.kind <> 'movie')
      OR (f.fetcher = 'metadata' AND ls.source = 'mdblist' AND k.kind IN ('movie', 'show'))
      OR (ls.source = 'omdb' AND k.kind IN ('movie', 'show'))
      OR (f.fetcher = 'metadata' AND ls.source = 'omdb' AND k.kind = 'episode'));
DELETE FROM library_sources WHERE item_kind IS NULL;
ALTER TABLE library_sources ALTER COLUMN item_kind SET NOT NULL,
  ALTER COLUMN fetcher SET NOT NULL,
  ALTER COLUMN enabled DROP DEFAULT,
  ADD PRIMARY KEY (library_id, item_kind, fetcher, source),
  ADD UNIQUE (library_id, item_kind, fetcher, position);

-- +goose Down
DELETE FROM library_sources WHERE NOT enabled
  OR fetcher <> 'metadata' OR item_kind NOT IN ('movie', 'show');
ALTER TABLE library_sources DROP CONSTRAINT library_sources_pkey,
  DROP CONSTRAINT library_sources_library_id_item_kind_fetcher_position_key,
  DROP COLUMN item_kind, DROP COLUMN fetcher, DROP COLUMN enabled;
UPDATE library_sources ls SET position = n.position
  FROM (SELECT library_id, source, row_number() OVER (PARTITION BY library_id ORDER BY position) - 1 AS position
    FROM library_sources) n
  WHERE n.library_id = ls.library_id AND n.source = ls.source;
ALTER TABLE library_sources ADD PRIMARY KEY (library_id, source), ADD UNIQUE (library_id, position);
