-- +goose Up
-- An invite lets whoever holds its code choose the first password of a profile added without one.
-- It is kept by its code's hash, never the code.
CREATE TABLE invites (
  code_hash bytea PRIMARY KEY,
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL
);
CREATE INDEX invites_profile_id ON invites (profile_id);

-- +goose Down
DROP TABLE invites;
