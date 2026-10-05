-- +goose Up
ALTER TABLE items DROP CONSTRAINT item_kind;
ALTER TABLE items ADD CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season', 'episode', 'extra'));
ALTER TABLE items ADD COLUMN extra_kind text CONSTRAINT extra_kind CHECK (extra_kind IN
  ('trailer', 'featurette', 'behind_the_scenes', 'deleted_scene', 'interview', 'scene', 'short',
   'clip', 'theme_video', 'other'));
ALTER TABLE items ADD CONSTRAINT extra_has_kind CHECK ((kind = 'extra') = (extra_kind IS NOT NULL));

-- +goose Down
DELETE FROM items WHERE kind = 'extra';
ALTER TABLE items DROP CONSTRAINT extra_has_kind, DROP COLUMN extra_kind;
ALTER TABLE items DROP CONSTRAINT item_kind;
ALTER TABLE items ADD CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season', 'episode'));
