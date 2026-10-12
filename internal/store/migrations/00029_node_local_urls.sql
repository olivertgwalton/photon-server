-- +goose Up
-- Where clients on the server's local networks reach a node themselves, as an admin sets it; none
-- is a node they reach at its public URL.
ALTER TABLE node ADD COLUMN local_url text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE node DROP COLUMN local_url;
