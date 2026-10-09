-- +goose Up
-- What each tracker a profile linked is yet to be told of the films and episodes it marked watched,
-- or unwatched since: watched_at is when it was watched, and none is unwatched. Rows are queued by
-- the triggers below as the profile's state changes, so no change is lost to a node stopping, and
-- one node at a time claims a row to tell, until claimed_until.
CREATE TABLE tracker_outbox (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  profile_id uuid NOT NULL,
  tracker text NOT NULL,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  watched_at timestamptz,
  queued_at timestamptz NOT NULL DEFAULT now(),
  claimed_until timestamptz,
  FOREIGN KEY (profile_id, tracker) REFERENCES tracker_accounts ON DELETE CASCADE
);
CREATE INDEX tracker_outbox_account ON tracker_outbox (profile_id, tracker);
CREATE INDEX tracker_outbox_item ON tracker_outbox (item_id);

-- +goose StatementBegin
CREATE FUNCTION tracker_outbox_queue() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO tracker_outbox (profile_id, tracker, item_id, watched_at)
  SELECT NEW.profile_id, a.tracker, NEW.item_id, NEW.watched_at
  FROM tracker_accounts a WHERE a.profile_id = NEW.profile_id;
  RETURN NULL;
END $$;
-- +goose StatementEnd

-- A title watched, or marked unwatched or watched again. A rewatch keeps when the title was first
-- watched, so it queues nothing: its scrobble tells it.
CREATE TRIGGER tracker_watched AFTER INSERT ON watch_state FOR EACH ROW
  WHEN (NEW.watched_at IS NOT NULL) EXECUTE FUNCTION tracker_outbox_queue();
CREATE TRIGGER tracker_marked AFTER UPDATE OF watched_at ON watch_state FOR EACH ROW
  WHEN (OLD.watched_at IS DISTINCT FROM NEW.watched_at) EXECUTE FUNCTION tracker_outbox_queue();

-- +goose Down
DROP TRIGGER tracker_marked ON watch_state;
DROP TRIGGER tracker_watched ON watch_state;
DROP FUNCTION tracker_outbox_queue();
DROP TABLE tracker_outbox;
