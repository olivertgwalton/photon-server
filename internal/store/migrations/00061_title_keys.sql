-- +goose Up
-- A title is the same title in every library that holds it, as Plex keeps one watch state for a
-- guid on a server. Its key is what Jellyfin keys user data by: a film's or show's provider id, a
-- season's or episode's show's and its numbers, and the content key of a film or episode none of
-- that names. A title with no key is only itself.
CREATE FUNCTION provider_key(item uuid) RETURNS text LANGUAGE sql STABLE PARALLEL SAFE RETURN (
  SELECT e.provider || ':' || e.value FROM external_ids e
  WHERE e.item_id = item AND e.provider IN ('tmdb', 'tvdb', 'imdb')
  ORDER BY array_position(ARRAY['tmdb', 'tvdb', 'imdb'], e.provider) LIMIT 1
);

CREATE FUNCTION title_key(item uuid) RETURNS text LANGUAGE sql STABLE PARALLEL SAFE RETURN (
  SELECT CASE i.kind
    WHEN 'season' THEN 'show/' || provider_key(i.parent_id) || '/' || i.season_number
    WHEN 'episode' THEN coalesce(
      'show/' || provider_key((SELECT s.parent_id FROM items s WHERE s.id = i.parent_id))
        || '/' || i.season_number || '/' || i.episode_number,
      'episode/file/' || encode((SELECT min(v.fingerprint) FROM versions v WHERE v.item_id = i.id), 'hex'))
    WHEN 'movie' THEN coalesce('movie/' || provider_key(i.id),
      'movie/file/' || encode((SELECT min(v.fingerprint) FROM versions v WHERE v.item_id = i.id), 'hex'))
    ELSE i.kind || '/' || provider_key(i.id)
  END
  FROM items i WHERE i.id = item
);

-- The keys as they were when a title was last saved or matched, as Jellyfin keeps an item's
-- presentation key: read on every page, they cannot be worked out on every page.
CREATE TABLE title_keys (
  item_id uuid PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  key text NOT NULL
);
CREATE INDEX title_keys_key ON title_keys (key);

-- Every title the same as one, itself included.
CREATE FUNCTION same_title(item uuid) RETURNS SETOF uuid LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT item
  UNION
  SELECT o.item_id FROM title_keys k JOIN title_keys o ON o.key = k.key WHERE k.item_id = item
$$;

-- Whether a title is the one of its kind a viewer is shown where every library's titles are
-- gathered: of those the same it may see, the one in the library added first.
CREATE FUNCTION first_of_title(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN NOT EXISTS (
  SELECT 1 FROM title_keys k JOIN title_keys o ON o.key = k.key JOIN items c ON c.id = o.item_id
  WHERE k.item_id = t.id AND (c.library_id, c.id) < (t.library_id, t.id) AND sees(v, c)
);

-- Keys titles, their seasons and episodes again, then makes every profile's state of each the same
-- wherever it is listed, merged as one row would have kept it: run where a title is saved or
-- matched, and so may have come to share a key.
-- +goose StatementBegin
CREATE FUNCTION key_titles(titles uuid[]) RETURNS void LANGUAGE sql AS $$
  WITH under AS (
    SELECT unnest(titles) AS id
    UNION
    SELECT s.id FROM items s WHERE s.parent_id = ANY (titles)
    UNION
    SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id WHERE s.parent_id = ANY (titles)
  ), keyed AS (
    SELECT under.id, title_key(under.id) AS key FROM under
  ), unkeyed AS (
    DELETE FROM title_keys k USING keyed WHERE k.item_id = keyed.id AND keyed.key IS NULL
  )
  INSERT INTO title_keys (item_id, key) SELECT id, key FROM keyed WHERE key IS NOT NULL ORDER BY id
  ON CONFLICT (item_id) DO UPDATE SET key = excluded.key WHERE title_keys.key <> excluded.key;

  WITH under AS (
    SELECT unnest(titles) AS id
    UNION
    SELECT s.id FROM items s WHERE s.parent_id = ANY (titles)
    UNION
    SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id WHERE s.parent_id = ANY (titles)
  ), pairs AS (
    SELECT k.item_id AS title, o.item_id AS same FROM under
    JOIN title_keys k ON k.item_id = under.id JOIN title_keys o ON o.key = k.key
    WHERE EXISTS (SELECT 1 FROM title_keys x WHERE x.key = k.key AND x.item_id <> k.item_id)
  ), merged AS (
    SELECT w.profile_id, p.title,
      (array_agg(w.position_ms ORDER BY w.last_played_at DESC NULLS LAST))[1] AS position_ms,
      max(w.plays) AS plays, min(w.watched_at) AS watched_at, max(w.last_played_at) AS last_played_at
    FROM pairs p JOIN watch_state w ON w.item_id = p.same
    GROUP BY w.profile_id, p.title
  ), states AS (
    INSERT INTO watch_state (profile_id, item_id, position_ms, plays, watched_at, last_played_at)
    SELECT DISTINCT ON (m.profile_id, p.same) m.profile_id, p.same, m.position_ms, m.plays, m.watched_at, m.last_played_at
    FROM merged m JOIN pairs p ON p.title = m.title
    ORDER BY m.profile_id, p.same
    ON CONFLICT (profile_id, item_id) DO UPDATE SET position_ms = excluded.position_ms, plays = excluded.plays,
      watched_at = excluded.watched_at, last_played_at = excluded.last_played_at
    WHERE (watch_state.position_ms, watch_state.plays, watch_state.watched_at, watch_state.last_played_at)
      IS DISTINCT FROM (excluded.position_ms, excluded.plays, excluded.watched_at, excluded.last_played_at)
  )
  INSERT INTO favourites (profile_id, item_id, added_at)
  SELECT f.profile_id, other.same, min(f.added_at)
  FROM pairs p JOIN favourites f ON f.item_id = p.same JOIN pairs other ON other.title = p.title
  GROUP BY f.profile_id, other.same
  ORDER BY f.profile_id, other.same
  ON CONFLICT (profile_id, item_id) DO NOTHING;
$$;
-- +goose StatementEnd

SELECT key_titles(ARRAY(SELECT id FROM items WHERE parent_id IS NULL));

-- +goose Down
DROP FUNCTION key_titles(uuid[]);
DROP FUNCTION first_of_title(viewer, items);
DROP FUNCTION same_title(uuid);
DROP TABLE title_keys;
DROP FUNCTION title_key(uuid);
DROP FUNCTION provider_key(uuid);
