-- +goose Up
-- Whether a video stream's picture is interlaced, by ffprobe's field order, to deinterlace it when
-- encoded.
ALTER TABLE streams ADD COLUMN interlaced boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE streams DROP COLUMN interlaced;
