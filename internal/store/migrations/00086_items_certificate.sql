-- +goose NO TRANSACTION
-- +goose Up
-- The certificates titles carry, each read once by skipping along this index, so a limited
-- profile's verdict on each is reached once a query rather than once a title.
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_certificate ON items (certificate) WHERE certificate IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS items_certificate;
