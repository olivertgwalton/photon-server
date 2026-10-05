-- +goose Up
CREATE TABLE server (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX server_singleton ON server ((true));
INSERT INTO server DEFAULT VALUES;

-- +goose Down
DROP TABLE server;
