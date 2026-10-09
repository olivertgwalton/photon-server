-- +goose Up
-- A library's media is the files under a folder, as ever, or, of a remote library, the streams a
-- provider offers for the titles of a list kept on a provider, found by their ids as one is
-- played. A remote library has no folder to watch or delete from.
ALTER TABLE libraries
  ADD COLUMN media text NOT NULL DEFAULT 'folder' CONSTRAINT library_media CHECK (media IN ('folder', 'remote')),
  ADD COLUMN list_source text,
  ADD COLUMN list_id text,
  ADD COLUMN stream_source text,
  ALTER COLUMN root DROP NOT NULL,
  ADD CONSTRAINT library_holding CHECK (CASE media
    WHEN 'folder' THEN root IS NOT NULL AND list_source IS NULL AND list_id IS NULL AND stream_source IS NULL
    ELSE root IS NULL AND list_source IS NOT NULL AND list_id IS NOT NULL AND stream_source IS NOT NULL END),
  ADD CONSTRAINT remote_library_untouched CHECK (media = 'folder' OR (monitor = 'off' AND deletion = 'off'));

-- +goose Down
DELETE FROM libraries WHERE media = 'remote';
ALTER TABLE libraries DROP CONSTRAINT remote_library_untouched, DROP CONSTRAINT library_holding,
  ALTER COLUMN root SET NOT NULL, DROP COLUMN stream_source, DROP COLUMN list_id, DROP COLUMN list_source,
  DROP COLUMN media;
