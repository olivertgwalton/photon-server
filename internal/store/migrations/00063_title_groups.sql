-- +goose Up
-- A title is keyed by every id it is known by, as Jellyfin keeps user data under each of a title's
-- keys: a film's, show's or box set's TMDB, TVDB and IMDb ids, a season's or episode's show's ids
-- with its numbers, and a film's or episode's files. Titles sharing a key are the same, and so is
-- anything sharing one with either: a show TMDB matched in one library is the show TheTVDB matched
-- in another once TMDB has given its TVDB id.
CREATE FUNCTION title_keys_of(item uuid) RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE AS $$
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

TRUNCATE title_keys;
ALTER TABLE title_keys DROP CONSTRAINT title_keys_pkey, ADD PRIMARY KEY (item_id, key);

-- Each title's group, the least id of the titles it is the same as: the keys joined once, when they
-- change, rather than on every page.
CREATE TABLE title_groups (
  item_id uuid PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  group_id uuid NOT NULL
);
CREATE INDEX title_groups_group ON title_groups (group_id);

CREATE OR REPLACE FUNCTION same_title(item uuid) RETURNS SETOF uuid LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT item
  UNION
  SELECT o.item_id FROM title_groups g JOIN title_groups o ON o.group_id = g.group_id WHERE g.item_id = item
$$;

CREATE OR REPLACE FUNCTION first_of_title(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN NOT EXISTS (
  SELECT 1 FROM title_groups g JOIN title_groups o ON o.group_id = g.group_id JOIN items c ON c.id = o.item_id
  WHERE g.item_id = t.id AND (c.library_id, c.id) < (t.library_id, t.id) AND sees(v, c)
);

DROP FUNCTION key_titles(uuid[]);
DROP FUNCTION title_key(uuid);
DROP FUNCTION provider_key(uuid);

-- Groups titles again, with those they were the same as before, then makes every profile's state of
-- each the same throughout its group, merged as one row would have kept it.
-- +goose StatementBegin
CREATE FUNCTION group_titles(titles uuid[]) RETURNS void LANGUAGE sql AS $$
  WITH RECURSIVE start AS (
    SELECT unnest(titles) AS id
    UNION
    SELECT o.item_id FROM title_groups g JOIN title_groups o ON o.group_id = g.group_id WHERE g.item_id = ANY (titles)
  ), reach (start, id) AS (
    SELECT start.id, start.id FROM start JOIN items i ON i.id = start.id
    UNION
    SELECT r.start, o.item_id FROM reach r
    JOIN title_keys k ON k.item_id = r.id JOIN title_keys o ON o.key = k.key JOIN items i ON i.id = o.item_id
  ), labelled AS (
    SELECT start, (array_agg(id ORDER BY id))[1] AS group_id FROM reach GROUP BY start
  )
  INSERT INTO title_groups (item_id, group_id)
  SELECT DISTINCT ON (r.id) r.id, l.group_id FROM reach r JOIN labelled l USING (start) ORDER BY r.id
  ON CONFLICT (item_id) DO UPDATE SET group_id = excluded.group_id WHERE title_groups.group_id <> excluded.group_id;

  WITH members AS (
    SELECT o.item_id, o.group_id FROM title_groups o
    WHERE o.group_id IN (SELECT g.group_id FROM title_groups g WHERE g.item_id = ANY (titles))
  ), shared AS (
    SELECT * FROM members WHERE group_id IN (SELECT group_id FROM members GROUP BY group_id HAVING count(*) > 1)
  ), merged AS (
    SELECT w.profile_id, s.group_id,
      (array_agg(w.position_ms ORDER BY w.last_played_at DESC NULLS LAST))[1] AS position_ms,
      max(w.plays) AS plays, min(w.watched_at) AS watched_at, max(w.last_played_at) AS last_played_at
    FROM shared s JOIN watch_state w ON w.item_id = s.item_id
    GROUP BY w.profile_id, s.group_id
  ), states AS (
    INSERT INTO watch_state (profile_id, item_id, position_ms, plays, watched_at, last_played_at)
    SELECT m.profile_id, s.item_id, m.position_ms, m.plays, m.watched_at, m.last_played_at
    FROM merged m JOIN shared s ON s.group_id = m.group_id
    ORDER BY m.profile_id, s.item_id
    ON CONFLICT (profile_id, item_id) DO UPDATE SET position_ms = excluded.position_ms, plays = excluded.plays,
      watched_at = excluded.watched_at, last_played_at = excluded.last_played_at
    WHERE (watch_state.position_ms, watch_state.plays, watch_state.watched_at, watch_state.last_played_at)
      IS DISTINCT FROM (excluded.position_ms, excluded.plays, excluded.watched_at, excluded.last_played_at)
  )
  INSERT INTO favourites (profile_id, item_id, added_at)
  SELECT f.profile_id, other.item_id, min(f.added_at)
  FROM shared s JOIN favourites f ON f.item_id = s.item_id JOIN shared other ON other.group_id = s.group_id
  GROUP BY f.profile_id, other.item_id
  ORDER BY f.profile_id, other.item_id
  ON CONFLICT (profile_id, item_id) DO NOTHING;
$$;
-- +goose StatementEnd

-- Keys titles, their seasons and episodes again and groups them: run where a title is saved or
-- matched, and so may have gained or lost an id.
-- +goose StatementBegin
CREATE FUNCTION key_titles(titles uuid[]) RETURNS void LANGUAGE sql AS $$
  WITH under AS (
    SELECT unnest(titles) AS id
    UNION
    SELECT s.id FROM items s WHERE s.parent_id = ANY (titles)
    UNION
    SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id WHERE s.parent_id = ANY (titles)
  ), keyed AS (
    SELECT under.id, k FROM under, title_keys_of(under.id) k
  ), unkeyed AS (
    DELETE FROM title_keys t USING under
    WHERE t.item_id = under.id AND NOT EXISTS (SELECT 1 FROM keyed WHERE keyed.id = t.item_id AND keyed.k = t.key)
  )
  INSERT INTO title_keys (item_id, key) SELECT id, k FROM keyed ORDER BY id, k ON CONFLICT DO NOTHING;

  SELECT group_titles(ARRAY(
    SELECT unnest(titles)
    UNION
    SELECT s.id FROM items s WHERE s.parent_id = ANY (titles)
    UNION
    SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id WHERE s.parent_id = ANY (titles)
  ));
$$;
-- +goose StatementEnd

-- A title that goes may have been all that joined others: what was its group is grouped again.
-- +goose StatementBegin
CREATE FUNCTION title_left() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM group_titles(ARRAY(SELECT g.item_id FROM title_groups g WHERE g.group_id IN (SELECT group_id FROM gone)));
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER title_left AFTER DELETE ON title_groups REFERENCING OLD TABLE AS gone
  FOR EACH STATEMENT EXECUTE FUNCTION title_left();

SELECT key_titles(ARRAY(SELECT id FROM items WHERE parent_id IS NULL));

-- +goose Down
DROP TRIGGER title_left ON title_groups;
DROP FUNCTION title_left();
DROP FUNCTION key_titles(uuid[]);
DROP FUNCTION group_titles(uuid[]);

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

TRUNCATE title_keys;
ALTER TABLE title_keys DROP CONSTRAINT title_keys_pkey, ADD PRIMARY KEY (item_id);

CREATE OR REPLACE FUNCTION same_title(item uuid) RETURNS SETOF uuid LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT item
  UNION
  SELECT o.item_id FROM title_keys k JOIN title_keys o ON o.key = k.key WHERE k.item_id = item
$$;

CREATE OR REPLACE FUNCTION first_of_title(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN NOT EXISTS (
  SELECT 1 FROM title_keys k JOIN title_keys o ON o.key = k.key JOIN items c ON c.id = o.item_id
  WHERE k.item_id = t.id AND (c.library_id, c.id) < (t.library_id, t.id) AND sees(v, c)
);

DROP TABLE title_groups;
DROP FUNCTION title_keys_of(uuid);

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
