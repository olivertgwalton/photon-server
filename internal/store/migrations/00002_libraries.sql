-- +goose Up
CREATE TABLE libraries (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  name text NOT NULL UNIQUE,
  kind text NOT NULL CONSTRAINT library_kind CHECK (kind IN ('movies', 'shows')),
  root text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE libraries;
