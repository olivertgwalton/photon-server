-- +goose Up
-- Where clients reach a node themselves, as an admin sets it; none is a node whose streams are
-- handed on through the node a client asks.
ALTER TABLE node ADD COLUMN public_url text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE node DROP COLUMN public_url;
