-- +goose Up
-- How a library finds its files' keyframes. Index is the default because it reads a few kilobytes
-- of any file, so a library on a network or debrid mount is never read whole to be scanned.
ALTER TABLE libraries ADD COLUMN keyframes text NOT NULL DEFAULT 'index'
  CONSTRAINT keyframe_mode CHECK (keyframes IN ('index', 'full', 'off'));

-- +goose Down
ALTER TABLE libraries DROP COLUMN keyframes;
