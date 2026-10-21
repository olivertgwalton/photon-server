-- +goose Up
-- A profile's authenticator app, whose code a password's sign-in also asks for. Step is the last
-- 30-second step a code was taken for, so no code is taken twice.
CREATE TABLE authenticators (
  profile_id uuid PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,
  secret bytea NOT NULL,
  step bigint NOT NULL,
  added_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE authenticators;
