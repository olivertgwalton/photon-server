-- +goose Up
ALTER TABLE libraries ADD COLUMN markers text NOT NULL DEFAULT 'all'
  CONSTRAINT marker_detection CHECK (markers IN ('off', 'chapters', 'all'));

-- +goose Down
ALTER TABLE libraries DROP COLUMN markers;
