-- +goose NO TRANSACTION
-- +goose Up
-- The films and shows a profile means to watch, as Plex's watchlist holds them. Each statement
-- may be run again, should one fail part way.
CREATE TABLE IF NOT EXISTS watchlist (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  added_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (profile_id, item_id)
);
CREATE INDEX IF NOT EXISTS watchlist_item ON watchlist (item_id);

-- As 00063's, with a title on the watchlist on it throughout its group, as a favourite is.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION group_titles(titles uuid[]) RETURNS void LANGUAGE sql AS $$
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
  ), listed AS (
    INSERT INTO watchlist (profile_id, item_id, added_at)
    SELECT l.profile_id, other.item_id, min(l.added_at)
    FROM shared s JOIN watchlist l ON l.item_id = s.item_id JOIN shared other ON other.group_id = s.group_id
    GROUP BY l.profile_id, other.item_id
    ORDER BY l.profile_id, other.item_id
    ON CONFLICT (profile_id, item_id) DO NOTHING
  )
  INSERT INTO favourites (profile_id, item_id, added_at)
  SELECT f.profile_id, other.item_id, min(f.added_at)
  FROM shared s JOIN favourites f ON f.item_id = s.item_id JOIN shared other ON other.group_id = s.group_id
  GROUP BY f.profile_id, other.item_id
  ORDER BY f.profile_id, other.item_id
  ON CONFLICT (profile_id, item_id) DO NOTHING;
$$;
-- +goose StatementEnd

-- A home may show the watchlist.
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN (
  'continue_watching', 'next_up', 'watchlist', 'favourites', 'recently_added_films', 'recently_added_shows',
  'recently_released', 'top_rated_unwatched', 'collection')) NOT VALID;
ALTER TABLE home_sections VALIDATE CONSTRAINT home_row;

-- +goose Down
DELETE FROM home_sections WHERE home_row = 'watchlist';
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN (
  'continue_watching', 'next_up', 'favourites', 'recently_added_films', 'recently_added_shows', 'recently_released',
  'top_rated_unwatched', 'collection')) NOT VALID;
ALTER TABLE home_sections VALIDATE CONSTRAINT home_row;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION group_titles(titles uuid[]) RETURNS void LANGUAGE sql AS $$
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

DROP TABLE IF EXISTS watchlist;
