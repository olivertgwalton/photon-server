-- +goose NO TRANSACTION
-- +goose Up
-- A provider's video on YouTube is pictured by the still YouTube publishes for it, fetched and
-- served by this server under thumb_id as any picture is.
ALTER TABLE remote_videos ADD COLUMN IF NOT EXISTS thumb_id uuid;
UPDATE remote_videos SET thumb_id = uuidv7() WHERE lower(site) = 'youtube' AND thumb_id IS NULL;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS remote_videos_thumb ON remote_videos (thumb_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS remote_videos_thumb;
ALTER TABLE remote_videos DROP COLUMN IF EXISTS thumb_id;
