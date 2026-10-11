-- +goose Up
-- The kind whose source ranking a title takes, as LibraryKind.RankedAs answers: its own, or for a
-- collection or an extra, its library's titles'.
-- +goose StatementBegin
CREATE FUNCTION ranked_as(library_kind text, item_kind text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN
  CASE
    WHEN library_kind = 'shows' AND item_kind IN ('show', 'season', 'episode') THEN item_kind
    WHEN library_kind = 'shows' THEN 'show'
    ELSE 'movie'
  END;
-- +goose StatementEnd

-- A title's pictures, each numbered by its place among the title's pictures of its kind, the best
-- first: an admin's choice, then files beside the title, then the providers its library asks for
-- pictures of the kind it is ranked as, in the library's order, then those it no longer asks, each
-- provider's own order breaking a tie.
-- +goose StatementBegin
CREATE FUNCTION ranked_artwork(titles uuid[]) RETURNS TABLE (id uuid, item_id uuid, source text, kind text,
  place text, "position" smallint, folder text, language text, width int, height int, blurhash text, nth bigint)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT a.id, a.item_id, a.source, a.kind, a.place, a.position, a.folder, a.language, a.width, a.height,
    a.blurhash, row_number() OVER (PARTITION BY a.item_id, a.kind ORDER BY
      CASE a.source WHEN 'user' THEN -2 WHEN 'file' THEN -1 ELSE coalesce(array_position(r.sources, a.source), 32767) END,
      a.position, a.id)
  FROM items i
  JOIN libraries l ON l.id = i.library_id
  -- The library's order is read once for each title, not once for each of its pictures.
  CROSS JOIN LATERAL (SELECT array_agg(ls.source ORDER BY ls.position) AS sources FROM library_sources ls
    WHERE ls.library_id = l.id AND ls.item_kind = ranked_as(l.kind, i.kind) AND ls.fetcher = 'images' AND ls.enabled) r
  JOIN artwork a ON a.item_id = i.id
  WHERE i.id = ANY(titles)
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION ranked_artwork(uuid[]);
DROP FUNCTION ranked_as(text, text);
