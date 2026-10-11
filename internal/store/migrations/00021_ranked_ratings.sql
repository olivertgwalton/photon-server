-- +goose Up
-- Each site's rating of a title, from the source its library ranks first for metadata of the kind
-- the title is ranked as, a source the library does not ask after every one it does. A card shows
-- it, and a wall and the home page sort and filter by it.
CREATE FUNCTION ranked_ratings(t items) RETURNS SETOF ratings LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT DISTINCT ON (r.site) r.* FROM ratings r
  JOIN libraries l ON l.id = t.library_id
  LEFT JOIN library_sources ls ON ls.library_id = l.id AND ls.fetcher = 'metadata' AND ls.enabled
    AND ls.source = r.source AND ls.item_kind = ranked_as(l.kind, t.kind)
  WHERE r.item_id = t.id
  ORDER BY r.site, ls.position, r.source
$$;

-- +goose Down
DROP FUNCTION ranked_ratings(items);
