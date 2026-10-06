-- +goose Up
-- A download is the device's that asked for it, as Plex's synced items are their client's: the
-- device lists its own, and signing it out forgets them. One from before is the device its
-- profile was last seen on.
ALTER TABLE downloads ADD COLUMN session_id uuid REFERENCES device_sessions(id) ON DELETE CASCADE;
UPDATE downloads d SET session_id = (
  SELECT s.id FROM device_sessions s WHERE s.profile_id = d.profile_id ORDER BY s.last_seen_at DESC LIMIT 1);
DELETE FROM downloads WHERE session_id IS NULL;
ALTER TABLE downloads ALTER COLUMN session_id SET NOT NULL,
  DROP CONSTRAINT downloads_profile_id_part_id_conversion_id_key,
  ADD CONSTRAINT downloads_profile_id_session_id_part_id_conversion_id_key
    UNIQUE NULLS NOT DISTINCT (profile_id, session_id, part_id, conversion_id);

-- +goose Down
DELETE FROM downloads d USING downloads o
WHERE o.profile_id = d.profile_id AND o.part_id = d.part_id
  AND o.conversion_id IS NOT DISTINCT FROM d.conversion_id AND o.id < d.id;
ALTER TABLE downloads DROP CONSTRAINT downloads_profile_id_session_id_part_id_conversion_id_key,
  ADD CONSTRAINT downloads_profile_id_part_id_conversion_id_key
    UNIQUE NULLS NOT DISTINCT (profile_id, part_id, conversion_id),
  DROP COLUMN session_id;
