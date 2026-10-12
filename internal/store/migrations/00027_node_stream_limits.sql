-- +goose Up
-- How many playbacks a node delivers at once, as an admin sets it; zero is no limit.
ALTER TABLE node ADD COLUMN stream_limit int NOT NULL DEFAULT 0 CONSTRAINT stream_limit CHECK (stream_limit >= 0);

-- +goose Down
ALTER TABLE node DROP COLUMN stream_limit;
