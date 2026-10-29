-- +goose Up
-- Whether a profile's players ask if anybody is still there after a run of episodes. One row per
-- profile, so nothing here waits on a large table.
ALTER TABLE profile_preferences ADD COLUMN still_watching text NOT NULL DEFAULT 'never'
  CONSTRAINT still_watching CHECK (still_watching IN ('ask', 'never'));

-- +goose Down
ALTER TABLE profile_preferences DROP COLUMN still_watching;
