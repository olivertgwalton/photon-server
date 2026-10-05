-- +goose Up
-- A video stream's bit depth and ffprobe's level, which decide whether a client can play it as it is.
ALTER TABLE streams ADD COLUMN bit_depth smallint, ADD COLUMN level int;

-- +goose Down
ALTER TABLE streams DROP COLUMN bit_depth, DROP COLUMN level;
