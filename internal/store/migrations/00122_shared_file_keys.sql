-- +goose Up
-- Bytes two titles of a library hold, as two episodes riven shows under each of their numbers,
-- key neither: they are two titles, not one known twice.
CREATE OR REPLACE FUNCTION title_keys_of(item uuid) RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT i.kind || '/' || e.provider || ':' || e.value
  FROM items i JOIN external_ids e ON e.item_id = i.id AND e.provider IN ('tmdb', 'tvdb', 'imdb')
  WHERE i.id = item AND i.kind IN ('movie', 'show', 'collection')
  UNION ALL
  SELECT i.kind || '/' || e.provider || ':' || e.value || '/' || i.season_number
    || CASE i.kind WHEN 'episode' THEN '/' || i.episode_number ELSE '' END
  FROM items i
  JOIN items show ON show.id = CASE i.kind WHEN 'season' THEN i.parent_id ELSE (SELECT s.parent_id FROM items s WHERE s.id = i.parent_id) END
  JOIN external_ids e ON e.item_id = show.id AND e.provider IN ('tmdb', 'tvdb', 'imdb')
  WHERE i.id = item AND i.kind IN ('season', 'episode') AND i.season_number IS NOT NULL
    AND (i.kind = 'season' OR i.episode_number IS NOT NULL)
  UNION ALL
  SELECT i.kind || '/file/' || encode(v.fingerprint, 'hex')
  FROM items i JOIN versions v ON v.item_id = i.id
  WHERE i.id = item AND i.kind IN ('movie', 'episode')
    AND NOT EXISTS (SELECT 1 FROM versions o WHERE o.library_id = v.library_id AND o.fingerprint = v.fingerprint AND o.item_id <> i.id)
$$;

-- +goose Down
CREATE OR REPLACE FUNCTION title_keys_of(item uuid) RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT i.kind || '/' || e.provider || ':' || e.value
  FROM items i JOIN external_ids e ON e.item_id = i.id AND e.provider IN ('tmdb', 'tvdb', 'imdb')
  WHERE i.id = item AND i.kind IN ('movie', 'show', 'collection')
  UNION ALL
  SELECT i.kind || '/' || e.provider || ':' || e.value || '/' || i.season_number
    || CASE i.kind WHEN 'episode' THEN '/' || i.episode_number ELSE '' END
  FROM items i
  JOIN items show ON show.id = CASE i.kind WHEN 'season' THEN i.parent_id ELSE (SELECT s.parent_id FROM items s WHERE s.id = i.parent_id) END
  JOIN external_ids e ON e.item_id = show.id AND e.provider IN ('tmdb', 'tvdb', 'imdb')
  WHERE i.id = item AND i.kind IN ('season', 'episode') AND i.season_number IS NOT NULL
    AND (i.kind = 'season' OR i.episode_number IS NOT NULL)
  UNION ALL
  SELECT i.kind || '/file/' || encode(v.fingerprint, 'hex')
  FROM items i JOIN versions v ON v.item_id = i.id
  WHERE i.id = item AND i.kind IN ('movie', 'episode')
$$;
