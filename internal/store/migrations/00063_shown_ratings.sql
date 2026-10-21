-- +goose Up
-- Each title's rating from each site, from the source its library ranks first for metadata of the
-- kind the title is ranked as, a source the library does not ask after every one it does: what a
-- card shows, and a wall and the home page sort and filter by. A view, so a wall sorted by a rating
-- ranks its library's at once, where asked of each title it took three times as long. The library
-- and the kind are keys it is ranked by, though the title alone decides them, so a wall's libraries
-- and kinds narrow what is ranked: a wall of shows does not rank their episodes'.
CREATE VIEW shown_ratings AS
  SELECT DISTINCT ON (r.item_id, t.library_id, t.kind, r.site) r.item_id, r.source, r.site, r.score, r.votes, t.library_id, t.kind
  FROM ratings r
  JOIN items t ON t.id = r.item_id
  JOIN libraries l ON l.id = t.library_id
  LEFT JOIN library_sources ls ON ls.library_id = l.id AND ls.fetcher = 'metadata' AND ls.enabled
    AND ls.source = r.source AND ls.item_kind = ranked_as(l.kind, t.kind)
  ORDER BY r.item_id, t.library_id, t.kind, r.site, ls.position, r.source;

-- Each source is asked after as a value: a join can't narrow the view to one title, and ranked
-- every title's ratings to find one title's.
CREATE OR REPLACE FUNCTION ranked_ratings(t items) RETURNS SETOF ratings LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT r.* FROM ratings r
  WHERE r.item_id = t.id AND r.source = (SELECT s.source FROM shown_ratings s WHERE s.item_id = t.id AND s.site = r.site)
$$;

-- +goose Down
CREATE OR REPLACE FUNCTION ranked_ratings(t items) RETURNS SETOF ratings LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT DISTINCT ON (r.site) r.* FROM ratings r
  JOIN libraries l ON l.id = t.library_id
  LEFT JOIN library_sources ls ON ls.library_id = l.id AND ls.fetcher = 'metadata' AND ls.enabled
    AND ls.source = r.source AND ls.item_kind = ranked_as(l.kind, t.kind)
  WHERE r.item_id = t.id
  ORDER BY r.site, ls.position, r.source
$$;

DROP VIEW shown_ratings;
