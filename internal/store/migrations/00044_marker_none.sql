-- +goose Up
-- An admin may say a part has no stretch of a kind: their row with no stretch, outranking what
-- the chapters and fingerprints found as their stretches do.
ALTER TABLE markers ALTER COLUMN start_ms DROP NOT NULL,
  ALTER COLUMN end_ms DROP NOT NULL,
  ADD CONSTRAINT marker_stretch CHECK ((start_ms IS NULL) = (end_ms IS NULL) AND (start_ms IS NOT NULL OR source = 'user'));

-- +goose Down
DELETE FROM markers WHERE start_ms IS NULL;
ALTER TABLE markers DROP CONSTRAINT marker_stretch,
  ALTER COLUMN start_ms SET NOT NULL,
  ALTER COLUMN end_ms SET NOT NULL;
