-- +goose NO TRANSACTION
-- +goose Up
-- A picture an admin uploads for a title ranks as one they chose of a provider's does, above every
-- source, and a title has one or the other of each kind, never both. The index is built beside the
-- server still writing.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS artwork_admins ON artwork (item_id, kind)
  WHERE source IN ('user', 'upload');
DROP INDEX CONCURRENTLY IF EXISTS artwork_chosen;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ranked_artwork(titles uuid[]) RETURNS TABLE (id uuid, item_id uuid, source text, kind text,
  place text, "position" smallint, folder text, language text, width int, height int, blurhash text, nth bigint)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT a.id, a.item_id, a.source, a.kind, a.place, a.position, a.folder, a.language, a.width, a.height,
    a.blurhash, row_number() OVER (PARTITION BY a.item_id, a.kind ORDER BY
      CASE a.source WHEN 'user' THEN -2 WHEN 'upload' THEN -2 WHEN 'file' THEN -1
        ELSE coalesce(array_position(r.sources, a.source), 32767) END,
      a.position, a.id)
  FROM items i
  LEFT JOIN libraries l ON l.id = i.library_id
  -- The library's order is read once for each title, not once for each of its pictures.
  CROSS JOIN LATERAL (SELECT array_agg(ls.source ORDER BY ls.position) AS sources FROM library_sources ls
    WHERE ls.library_id = l.id AND ls.item_kind = ranked_as(l.kind, i.kind) AND ls.fetcher = 'images' AND ls.enabled) r
  JOIN artwork a ON a.item_id = i.id
  WHERE i.id = ANY(titles)
$$;
-- +goose StatementEnd

-- +goose Down
DELETE FROM artwork WHERE source = 'upload';
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ranked_artwork(titles uuid[]) RETURNS TABLE (id uuid, item_id uuid, source text, kind text,
  place text, "position" smallint, folder text, language text, width int, height int, blurhash text, nth bigint)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT a.id, a.item_id, a.source, a.kind, a.place, a.position, a.folder, a.language, a.width, a.height,
    a.blurhash, row_number() OVER (PARTITION BY a.item_id, a.kind ORDER BY
      CASE a.source WHEN 'user' THEN -2 WHEN 'file' THEN -1 ELSE coalesce(array_position(r.sources, a.source), 32767) END,
      a.position, a.id)
  FROM items i
  LEFT JOIN libraries l ON l.id = i.library_id
  -- The library's order is read once for each title, not once for each of its pictures.
  CROSS JOIN LATERAL (SELECT array_agg(ls.source ORDER BY ls.position) AS sources FROM library_sources ls
    WHERE ls.library_id = l.id AND ls.item_kind = ranked_as(l.kind, i.kind) AND ls.fetcher = 'images' AND ls.enabled) r
  JOIN artwork a ON a.item_id = i.id
  WHERE i.id = ANY(titles)
$$;
-- +goose StatementEnd
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS artwork_chosen ON artwork (item_id, kind) WHERE source = 'user';
DROP INDEX CONCURRENTLY IF EXISTS artwork_admins;
