-- +goose Up
CREATE TABLE profiles (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  name text NOT NULL UNIQUE,
  role text NOT NULL CONSTRAINT profile_role CHECK (role IN ('admin', 'member', 'restricted')),
  password_hash text,
  pin_hash text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE device_sessions (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  token_hash bytea NOT NULL UNIQUE,
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  device_name text NOT NULL,
  client text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);
CREATE INDEX device_sessions_profile ON device_sessions (profile_id);

-- +goose Down
DROP TABLE device_sessions, profiles;
