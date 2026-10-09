-- +goose Up
-- The app an admin registered on each tracker, which every profile's account is linked through.
CREATE TABLE trackers (
  tracker text PRIMARY KEY CONSTRAINT tracker CHECK (tracker IN ('trakt', 'simkl')),
  client_id text NOT NULL
);

-- The account a profile linked on a tracker, and what the tracker granted it.
CREATE TABLE tracker_accounts (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  tracker text NOT NULL CONSTRAINT account_tracker CHECK (tracker IN ('trakt', 'simkl')),
  username text NOT NULL,
  access_token text NOT NULL,
  refresh_token text NOT NULL,
  expires_at timestamptz NOT NULL,
  linked_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (profile_id, tracker)
);

-- +goose Down
DROP TABLE tracker_accounts;
DROP TABLE trackers;
