-- +goose Up
-- Where a library finds theme tunes. Plex's TV agent takes its theme host's by default, so every
-- library, new or not, does too; a film has none there, so all asks no more of a movies library.
ALTER TABLE libraries ADD COLUMN themes text NOT NULL DEFAULT 'all'
  CONSTRAINT theme_lookup CHECK (themes IN ('all', 'local', 'off'));

-- Whether a profile's pages play their theme tunes; off, as Jellyfin's web client.
ALTER TABLE profile_preferences ADD COLUMN theme_music text NOT NULL DEFAULT 'off'
  CONSTRAINT theme_music CHECK (theme_music IN ('play', 'off'));

-- A film's or show's theme tunes, as Jellyfin's theme songs: files in its folder, kept as the scan
-- of that folder finds them, or the one fetched from Plex's theme host into the server's cache
-- under the row's id, as place says where from.
CREATE TABLE themes (
  id uuid NOT NULL UNIQUE DEFAULT uuidv7(),
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL CONSTRAINT theme_source CHECK (source IN ('file', 'tvthemes')),
  place text NOT NULL,
  position smallint NOT NULL,
  folder text,
  PRIMARY KEY (item_id, source, place),
  CONSTRAINT theme_file_has_folder CHECK ((source = 'file') = (folder IS NOT NULL))
);

ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook', 'theme'));

-- +goose Down
DELETE FROM jobs WHERE kind = 'theme';
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook'));
DROP TABLE themes;
ALTER TABLE profile_preferences DROP COLUMN theme_music;
ALTER TABLE libraries DROP COLUMN themes;
