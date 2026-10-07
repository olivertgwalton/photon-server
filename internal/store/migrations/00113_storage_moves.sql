-- +goose Up
-- Artwork and previews being moved to another place: at most one move at a time, and how far each
-- copy has got, from a node's own disk or, with the nil id, from the bucket every node shares.
CREATE TABLE storage_move (
  one boolean PRIMARY KEY DEFAULT true CONSTRAINT one_move CHECK (one),
  target jsonb NOT NULL,
  started timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE storage_move_sources (
  source uuid PRIMARY KEY,
  move boolean NOT NULL DEFAULT true REFERENCES storage_move ON DELETE CASCADE,
  copied integer NOT NULL DEFAULT 0,
  total integer NOT NULL DEFAULT 0,
  done boolean NOT NULL DEFAULT false,
  seen timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX storage_move_sources_move ON storage_move_sources (move);

-- +goose Down
DROP TABLE storage_move_sources;
DROP TABLE storage_move;
