-- +goose Up
-- The server nodes there are or have been, and what an admin sets of each: its role, and its
-- limit on transcodes at once, worked out from its encoder where none is set, and none at 0.
CREATE TABLE node (
  id uuid PRIMARY KEY,
  name text NOT NULL,
  first_seen timestamptz NOT NULL DEFAULT now(),
  role text NOT NULL DEFAULT 'all' CONSTRAINT role CHECK (role IN ('all', 'serve', 'transcode')),
  transcode_limit integer CONSTRAINT transcode_limit CHECK (transcode_limit >= 0)
);

-- +goose Down
DROP TABLE node;
