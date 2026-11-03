-- +goose NO TRANSACTION
-- +goose Up
-- An id may come from a sound file's tags, as Picard writes a MusicBrainz id. The check is checked
-- again apart from its change, reading the table without stopping its writes. A music library
-- asks MusicBrainz and fanart.tv of its artists and albums, as one added now does.
ALTER TABLE external_ids DROP CONSTRAINT id_source,
  ADD CONSTRAINT id_source CHECK (source IN ('match', 'nfo', 'tags', 'path', 'user')) NOT VALID;
ALTER TABLE external_ids VALIDATE CONSTRAINT id_source;
INSERT INTO library_sources (library_id, item_kind, fetcher, source, position, enabled)
SELECT l.id, s.item_kind, s.fetcher, s.source, s.position, true FROM libraries l,
  (VALUES ('artist', 'metadata', 'musicbrainz', 0), ('artist', 'images', 'fanart', 0),
    ('album', 'metadata', 'musicbrainz', 0), ('album', 'images', 'musicbrainz', 0), ('album', 'images', 'fanart', 1))
    AS s (item_kind, fetcher, source, position)
WHERE l.kind = 'music'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM library_sources WHERE source IN ('musicbrainz', 'fanart')
  AND library_id IN (SELECT id FROM libraries WHERE kind = 'music');
DELETE FROM external_ids WHERE source = 'tags';
ALTER TABLE external_ids DROP CONSTRAINT id_source,
  ADD CONSTRAINT id_source CHECK (source IN ('match', 'nfo', 'path', 'user')) NOT VALID;
ALTER TABLE external_ids VALIDATE CONSTRAINT id_source;
