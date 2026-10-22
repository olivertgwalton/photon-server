-- +goose Up
-- Whether a profile's apps blur the still of an episode it has not started. One row per profile, so
-- nothing here waits on a large table.
ALTER TABLE profile_preferences ADD COLUMN episode_spoilers text NOT NULL DEFAULT 'show'
  CONSTRAINT episode_spoilers CHECK (episode_spoilers IN ('show', 'hide'));

-- +goose Down
ALTER TABLE profile_preferences DROP COLUMN episode_spoilers;
