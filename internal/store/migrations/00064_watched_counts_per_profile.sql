-- +goose Up
-- A watch state counts towards its own profile's counts alone, and never towards a group's
-- episodes: each progress saved recounted both for every profile, nine tenths of the time a save
-- took. A count already right is left as it is, so a save rewrites one row of watched_counts, and
-- in place, where it deleted and wrote again four.
-- +goose StatementBegin
CREATE FUNCTION keep_watched_counts(groups uuid[], profiles uuid[]) RETURNS void LANGUAGE sql AS $$
  SELECT pg_advisory_xact_lock(1702389, hashtext(g::text)) FROM unnest(groups) g ORDER BY g;
  WITH counted AS (
    SELECT ws.profile_id, ge.group_id, count(ws.watched_at) AS watched, max(ws.last_played_at) AS last_played_at,
      max(ws.watched_at) AS watched_at
    FROM group_episodes(groups) ge JOIN watch_state ws ON ws.item_id = ge.episode_id
    WHERE profiles IS NULL OR ws.profile_id = ANY(profiles)
    GROUP BY ws.profile_id, ge.group_id
  ), kept AS (
    INSERT INTO watched_counts (profile_id, group_id, watched, last_played_at, watched_at)
    SELECT * FROM counted
    ON CONFLICT (profile_id, group_id) DO UPDATE SET watched = excluded.watched,
      last_played_at = excluded.last_played_at, watched_at = excluded.watched_at
    WHERE (watched_counts.watched, watched_counts.last_played_at, watched_counts.watched_at)
      IS DISTINCT FROM (excluded.watched, excluded.last_played_at, excluded.watched_at)
  )
  DELETE FROM watched_counts w
  WHERE w.group_id = ANY(groups) AND (profiles IS NULL OR w.profile_id = ANY(profiles))
    AND NOT EXISTS (SELECT FROM counted c WHERE c.profile_id = w.profile_id AND c.group_id = w.group_id);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION keep_episode_counts(groups uuid[]) RETURNS void LANGUAGE sql AS $$
  SELECT pg_advisory_xact_lock(1702389, hashtext(g::text)) FROM unnest(groups) g ORDER BY g;
  DELETE FROM episode_counts WHERE group_id = ANY(groups);
  INSERT INTO episode_counts (group_id, episodes)
  SELECT group_id, count(*) FROM group_episodes(groups) GROUP BY group_id;
  SELECT keep_watched_counts(groups, NULL);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION watch_state_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  groups uuid[];
  profiles uuid[];
BEGIN
  IF TG_OP = 'INSERT' THEN
    groups := groups_of(ARRAY(SELECT i.parent_id FROM added a JOIN items i ON i.id = a.item_id WHERE i.kind = 'episode'));
    profiles := ARRAY(SELECT DISTINCT profile_id FROM added);
  ELSIF TG_OP = 'DELETE' THEN
    groups := groups_of(ARRAY(SELECT i.parent_id FROM gone a JOIN items i ON i.id = a.item_id WHERE i.kind = 'episode'));
    profiles := ARRAY(SELECT DISTINCT profile_id FROM gone);
  ELSE
    -- A changed audio or subtitle choice counts towards nothing.
    groups := groups_of(ARRAY(SELECT i.parent_id FROM added a JOIN gone o USING (profile_id, item_id)
      JOIN items i ON i.id = a.item_id
      WHERE i.kind = 'episode' AND (o.watched_at, o.last_played_at) IS DISTINCT FROM (a.watched_at, a.last_played_at)));
    profiles := ARRAY(SELECT DISTINCT profile_id FROM added);
  END IF;
  IF cardinality(groups) > 0 THEN
    PERFORM keep_watched_counts(groups, profiles);
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION watch_state_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  groups uuid[];
BEGIN
  IF TG_OP = 'INSERT' THEN
    groups := groups_of(ARRAY(SELECT i.parent_id FROM added a JOIN items i ON i.id = a.item_id WHERE i.kind = 'episode'));
  ELSIF TG_OP = 'DELETE' THEN
    groups := groups_of(ARRAY(SELECT i.parent_id FROM gone a JOIN items i ON i.id = a.item_id WHERE i.kind = 'episode'));
  ELSE
    -- A changed audio or subtitle choice counts towards nothing.
    groups := groups_of(ARRAY(SELECT i.parent_id FROM added a JOIN gone o USING (profile_id, item_id)
      JOIN items i ON i.id = a.item_id
      WHERE i.kind = 'episode' AND (o.watched_at, o.last_played_at) IS DISTINCT FROM (a.watched_at, a.last_played_at)));
  END IF;
  IF cardinality(groups) > 0 THEN
    PERFORM keep_episode_counts(groups);
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION keep_episode_counts(groups uuid[]) RETURNS void LANGUAGE sql AS $$
  SELECT pg_advisory_xact_lock(1702389, hashtext(g::text)) FROM unnest(groups) g ORDER BY g;
  DELETE FROM episode_counts WHERE group_id = ANY(groups);
  INSERT INTO episode_counts (group_id, episodes)
  SELECT group_id, count(*) FROM group_episodes(groups) GROUP BY group_id;
  DELETE FROM watched_counts WHERE group_id = ANY(groups);
  INSERT INTO watched_counts (profile_id, group_id, watched, last_played_at, watched_at)
  SELECT ws.profile_id, ge.group_id, count(ws.watched_at), max(ws.last_played_at), max(ws.watched_at)
  FROM group_episodes(groups) ge JOIN watch_state ws ON ws.item_id = ge.episode_id
  GROUP BY ws.profile_id, ge.group_id;
$$;
-- +goose StatementEnd

DROP FUNCTION keep_watched_counts(uuid[], uuid[]);
