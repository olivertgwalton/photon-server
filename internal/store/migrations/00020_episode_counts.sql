-- +goose NO TRANSACTION
-- +goose Up
-- A show's or season's card says how many of its episodes are left and when one was last played.
-- Counting its episodes and each one's watch state as each card was drawn was most of the time a
-- wall of shows took, so the counts are kept: by triggers as episodes and watch states change. Each
-- statement is its own transaction, so a trigger locks its table only while it is made.
CREATE TABLE episode_counts (
  group_id uuid PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  episodes int NOT NULL
);
CREATE TABLE watched_counts (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  group_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  watched int NOT NULL,
  last_played_at timestamptz,
  watched_at timestamptz,
  PRIMARY KEY (profile_id, group_id)
);
CREATE INDEX watched_counts_group ON watched_counts (group_id);

-- A season's episodes are its own; a show's are those of its seasons.
-- +goose StatementBegin
CREATE FUNCTION group_episodes(groups uuid[]) RETURNS TABLE (group_id uuid, episode_id uuid)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT e.parent_id, e.id FROM items e WHERE e.parent_id = ANY(groups) AND e.kind = 'episode'
  UNION ALL
  SELECT s.parent_id, e.id FROM items s JOIN items e ON e.parent_id = s.id
  WHERE s.parent_id = ANY(groups) AND e.kind = 'episode'
$$;
-- +goose StatementEnd

-- A group is locked first, under a lock of its own rather than its row so a progress saved waits on
-- nothing a scan holds, and two transactions changing one group count it in turn, the second
-- seeing what the first saved.
-- +goose StatementBegin
CREATE FUNCTION keep_episode_counts(groups uuid[]) RETURNS void LANGUAGE sql AS $$
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

-- The groups an episode or season counts towards: its parent and its parent's parent.
-- +goose StatementBegin
CREATE FUNCTION groups_of(titles uuid[]) RETURNS uuid[] LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT coalesce(array_agg(DISTINCT g), '{}') FROM (
    SELECT p.id AS g FROM items p WHERE p.id = ANY(titles) AND p.kind IN ('show', 'season')
    UNION SELECT p.parent_id FROM items p WHERE p.id = ANY(titles) AND p.kind = 'season'
  ) x WHERE g IS NOT NULL
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION watch_state_changed() RETURNS trigger LANGUAGE plpgsql AS $$
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

-- An episode or season added, removed or moved changes the counts of the groups it was and is in.
-- +goose StatementBegin
CREATE FUNCTION items_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  groups uuid[];
BEGIN
  IF TG_OP = 'INSERT' THEN
    groups := groups_of(ARRAY(SELECT parent_id FROM added WHERE kind IN ('episode', 'season')));
  ELSIF TG_OP = 'DELETE' THEN
    groups := groups_of(ARRAY(SELECT parent_id FROM gone WHERE kind IN ('episode', 'season')));
  ELSE
    groups := groups_of(ARRAY(
      SELECT p FROM gone o JOIN added n USING (id), LATERAL (VALUES (o.parent_id), (n.parent_id)) v(p)
      WHERE n.kind IN ('episode', 'season') AND o.parent_id IS DISTINCT FROM n.parent_id));
  END IF;
  IF cardinality(groups) > 0 THEN
    PERFORM keep_episode_counts(groups);
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER watch_state_added AFTER INSERT ON watch_state REFERENCING NEW TABLE AS added
  FOR EACH STATEMENT EXECUTE FUNCTION watch_state_changed();
CREATE TRIGGER watch_state_gone AFTER DELETE ON watch_state REFERENCING OLD TABLE AS gone
  FOR EACH STATEMENT EXECUTE FUNCTION watch_state_changed();
CREATE TRIGGER watch_state_updated AFTER UPDATE ON watch_state REFERENCING OLD TABLE AS gone NEW TABLE AS added
  FOR EACH STATEMENT EXECUTE FUNCTION watch_state_changed();
CREATE TRIGGER items_added AFTER INSERT ON items REFERENCING NEW TABLE AS added
  FOR EACH STATEMENT EXECUTE FUNCTION items_changed();
CREATE TRIGGER items_gone AFTER DELETE ON items REFERENCING OLD TABLE AS gone
  FOR EACH STATEMENT EXECUTE FUNCTION items_changed();
CREATE TRIGGER items_updated AFTER UPDATE ON items REFERENCING OLD TABLE AS gone NEW TABLE AS added
  FOR EACH STATEMENT EXECUTE FUNCTION items_changed();

-- The shows are counted a hundred at a time with their seasons, each hundred committed as it is
-- done, so no group is locked longer than that beside the server still saving.
-- +goose StatementBegin
DO $$
DECLARE
  shows uuid[];
  last uuid := '00000000-0000-0000-0000-000000000000';
BEGIN
  LOOP
    SELECT array_agg(id ORDER BY id) INTO shows
    FROM (SELECT id FROM items WHERE kind = 'show' AND id > last ORDER BY id LIMIT 100) t;
    EXIT WHEN shows IS NULL;
    PERFORM keep_episode_counts(shows || ARRAY(SELECT id FROM items WHERE parent_id = ANY(shows) AND kind = 'season'));
    last := shows[cardinality(shows)];
    COMMIT;
  END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER items_updated ON items;
DROP TRIGGER items_gone ON items;
DROP TRIGGER items_added ON items;
DROP TRIGGER watch_state_updated ON watch_state;
DROP TRIGGER watch_state_gone ON watch_state;
DROP TRIGGER watch_state_added ON watch_state;
DROP FUNCTION items_changed();
DROP FUNCTION watch_state_changed();
DROP FUNCTION groups_of(uuid[]);
DROP FUNCTION keep_episode_counts(uuid[]);
DROP FUNCTION group_episodes(uuid[]);
DROP TABLE watched_counts;
DROP TABLE episode_counts;
