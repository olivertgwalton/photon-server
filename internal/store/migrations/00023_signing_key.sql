-- +goose Up
-- The key the server signs stream addresses with: 32 bytes from gen_random_uuid, which Postgres
-- draws from its cryptographic random source.
ALTER TABLE server ADD COLUMN signing_key bytea NOT NULL
  DEFAULT decode(replace(gen_random_uuid()::text || gen_random_uuid()::text, '-', ''), 'hex');

-- +goose Down
ALTER TABLE server DROP COLUMN signing_key;
