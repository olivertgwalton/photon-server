-- +goose Up
-- Each playback as it stopped: who played what, which copy and how, from when to when, and how far.
CREATE TABLE plays (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  version_id uuid REFERENCES versions(id) ON DELETE SET NULL,
  method text NOT NULL CONSTRAINT play_method CHECK (method IN ('direct', 'remux', 'transcode')),
  started_at timestamptz NOT NULL,
  stopped_at timestamptz NOT NULL,
  position_ms bigint NOT NULL
);
CREATE INDEX plays_profile ON plays (profile_id, stopped_at DESC);
CREATE INDEX plays_stopped ON plays (stopped_at DESC);

-- +goose Down
DROP TABLE plays;
