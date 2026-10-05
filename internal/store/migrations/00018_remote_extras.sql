-- +goose Up
ALTER TABLE items DROP CONSTRAINT extra_kind, ADD CONSTRAINT extra_kind CHECK (extra_kind IN
  ('trailer', 'teaser', 'featurette', 'behind_the_scenes', 'deleted_scene', 'interview', 'scene',
   'short', 'clip', 'blooper', 'theme_video', 'other'));

CREATE TABLE library_remote_extras (
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  kind text NOT NULL CONSTRAINT library_extra_kind CHECK (kind IN
    ('trailer', 'teaser', 'featurette', 'behind_the_scenes', 'deleted_scene', 'interview', 'scene',
     'short', 'clip', 'blooper', 'theme_video', 'other')),
  PRIMARY KEY (library_id, kind)
);
INSERT INTO library_remote_extras (library_id, kind)
  SELECT id, k FROM libraries, unnest(ARRAY['trailer', 'featurette', 'behind_the_scenes']) k;

-- A provider's link to a video it hosts elsewhere, replaced whole whenever the provider is asked.
CREATE TABLE remote_videos (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL CONSTRAINT remote_video_source CHECK (source IN ('tmdb', 'tvdb')),
  position smallint NOT NULL,
  kind text NOT NULL CONSTRAINT remote_video_kind CHECK (kind IN
    ('trailer', 'teaser', 'featurette', 'behind_the_scenes', 'deleted_scene', 'interview', 'scene',
     'short', 'clip', 'blooper', 'theme_video', 'other')),
  site text NOT NULL,
  key text NOT NULL,
  name text NOT NULL,
  language text,
  published_at timestamptz,
  PRIMARY KEY (item_id, source, position)
);

-- +goose Down
DROP TABLE remote_videos, library_remote_extras;
UPDATE items SET extra_kind = 'trailer' WHERE extra_kind = 'teaser';
UPDATE items SET extra_kind = 'other' WHERE extra_kind = 'blooper';
ALTER TABLE items DROP CONSTRAINT extra_kind, ADD CONSTRAINT extra_kind CHECK (extra_kind IN
  ('trailer', 'featurette', 'behind_the_scenes', 'deleted_scene', 'interview', 'scene', 'short',
   'clip', 'theme_video', 'other'));
