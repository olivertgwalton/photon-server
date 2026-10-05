-- +goose Up
ALTER TABLE external_ids DROP CONSTRAINT id_source,
  ADD CONSTRAINT id_source CHECK (source IN ('nfo', 'path'));

-- +goose Down
DELETE FROM external_ids WHERE source = 'nfo';
ALTER TABLE external_ids DROP CONSTRAINT id_source,
  ADD CONSTRAINT id_source CHECK (source IN ('path'));
