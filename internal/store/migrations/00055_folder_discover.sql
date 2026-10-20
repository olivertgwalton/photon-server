-- +goose Up
-- A library of files may search a provider too: what it finds that no library holds is a title a
-- profile may ask for.
ALTER TABLE libraries DROP CONSTRAINT library_holding,
  ADD CONSTRAINT library_holding CHECK (CASE media
    WHEN 'folder' THEN root IS NOT NULL AND list_source IS NULL AND list_id IS NULL AND stream_source IS NULL
    ELSE root IS NULL AND (list_source IS NULL) = (list_id IS NULL) AND stream_source IS NOT NULL
      AND (list_source IS NOT NULL OR discover_source IS NOT NULL) END);

-- +goose Down
DELETE FROM discoveries d USING libraries l WHERE l.id = d.library_id AND l.media = 'folder';
UPDATE libraries SET discover_source = NULL WHERE media = 'folder';
ALTER TABLE libraries DROP CONSTRAINT library_holding,
  ADD CONSTRAINT library_holding CHECK (CASE media
    WHEN 'folder' THEN root IS NOT NULL AND list_source IS NULL AND list_id IS NULL AND stream_source IS NULL
      AND discover_source IS NULL
    ELSE root IS NULL AND (list_source IS NULL) = (list_id IS NULL) AND stream_source IS NOT NULL
      AND (list_source IS NOT NULL OR discover_source IS NOT NULL) END);
