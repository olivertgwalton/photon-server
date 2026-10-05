-- +goose Up
-- What a profile has watched of a film or episode and where it stopped; a show's and a season's
-- are read from their episodes'. A title watched has watched_at; one under way has a position.
CREATE TABLE watch_state (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  position_ms bigint NOT NULL DEFAULT 0 CONSTRAINT position_not_negative CHECK (position_ms >= 0),
  plays int NOT NULL DEFAULT 0,
  watched_at timestamptz,
  last_played_at timestamptz,
  PRIMARY KEY (profile_id, item_id)
);
CREATE INDEX watch_state_resume ON watch_state (profile_id, last_played_at DESC) WHERE position_ms > 0;

CREATE TABLE favourites (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  added_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (profile_id, item_id)
);

-- +goose Down
DROP TABLE favourites, watch_state;
