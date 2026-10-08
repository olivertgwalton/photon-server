-- +goose Up
-- Where the other nodes reach a node, as an admin sets it; none is a node handed no one else's
-- requests.
ALTER TABLE node ADD COLUMN address text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE node DROP COLUMN address;
