-- +goose Up
-- An id an admin pinned a title to, above any other.
ALTER TABLE external_ids DROP CONSTRAINT id_source,
  ADD CONSTRAINT id_source CHECK (source IN ('match', 'nfo', 'path', 'user'));

-- +goose Down
DELETE FROM external_ids WHERE source = 'user';
ALTER TABLE external_ids DROP CONSTRAINT id_source,
  ADD CONSTRAINT id_source CHECK (source IN ('match', 'nfo', 'path'));
