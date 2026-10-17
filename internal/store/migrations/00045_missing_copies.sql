-- +goose Up
-- A library says what becomes of a copy on no disk at the end of a scan: kept, marked missing so
-- a mount down for repair does not cost its titles, as Plex keeps a removed item until its trash
-- is emptied; or forgotten there and then, as Plex's "Empty trash automatically after every scan"
-- does. A remote library is never scanned, so it keeps them.
ALTER TABLE libraries
  ADD COLUMN missing_copies text NOT NULL DEFAULT 'keep'
    CONSTRAINT missing_copy CHECK (missing_copies IN ('keep', 'forget')),
  DROP CONSTRAINT remote_library_untouched,
  ADD CONSTRAINT remote_library_untouched CHECK (media = 'folder'
    OR (monitor = 'off' AND deletion = 'off' AND missing_copies = 'keep'));

-- +goose Down
ALTER TABLE libraries DROP CONSTRAINT remote_library_untouched,
  ADD CONSTRAINT remote_library_untouched CHECK (media = 'folder' OR (monitor = 'off' AND deletion = 'off')),
  DROP COLUMN missing_copies;
