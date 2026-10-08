-- +goose Up
-- How a profile's app lays out each of its views, kept as the app sent it: the server reads none of
-- it.
CREATE TABLE display_preferences (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  client text NOT NULL,
  view text NOT NULL,
  preferences jsonb NOT NULL,
  PRIMARY KEY (profile_id, client, view)
);

-- +goose Down
DROP TABLE display_preferences;
