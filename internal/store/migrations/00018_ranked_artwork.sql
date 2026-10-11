-- +goose Up
-- A title's pictures, each with its rank: an admin's choice, then files beside the title, then the
-- providers its library asks for pictures of the kind it is ranked as, in the library's order,
-- then those it no longer asks. A collection or an extra is ranked as its library's titles, as
-- LibraryKind.RankedAs ranks them.
-- +goose StatementBegin
CREATE FUNCTION ranked_artwork(titles uuid[]) RETURNS TABLE (id uuid, item_id uuid, source text, kind text,
  place text, "position" smallint, folder text, language text, width int, height int, blurhash text, rank int)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT a.id, a.item_id, a.source, a.kind, a.place, a.position, a.folder, a.language, a.width, a.height,
    a.blurhash, CASE a.source WHEN 'user' THEN -2 WHEN 'file' THEN -1 ELSE coalesce(ls.position, 32767) END
  FROM items i
  JOIN libraries l ON l.id = i.library_id
  JOIN artwork a ON a.item_id = i.id
  LEFT JOIN library_sources ls ON ls.library_id = l.id AND ls.fetcher = 'images' AND ls.enabled
    AND ls.source = a.source AND ls.item_kind = CASE
      WHEN l.kind = 'shows' AND i.kind IN ('show', 'season', 'episode') THEN i.kind
      WHEN l.kind = 'shows' THEN 'show'
      ELSE 'movie' END
  WHERE i.id = ANY(titles)
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION ranked_artwork(uuid[]);
