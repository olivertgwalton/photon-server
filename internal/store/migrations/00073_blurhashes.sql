-- +goose Up
-- A picture's BlurHash, for a client to draw blurred while the picture loads, as Jellyfin's
-- ImageBlurHashes. Taken when the picture is first fetched or scanned; null until then.
ALTER TABLE artwork ADD COLUMN blurhash text;
ALTER TABLE people ADD COLUMN photo_blurhash text;

-- +goose Down
ALTER TABLE people DROP COLUMN photo_blurhash;
ALTER TABLE artwork DROP COLUMN blurhash;
