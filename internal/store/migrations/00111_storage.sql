-- +goose Up
-- Where artwork and previews are kept: each node's own cache folder, or an S3 bucket every node
-- shares. The secret key is never sent back to a client.
ALTER TABLE server
  ADD COLUMN storage text NOT NULL DEFAULT 'disk' CONSTRAINT storage CHECK (storage IN ('disk', 'bucket')),
  ADD COLUMN bucket_endpoint text NOT NULL DEFAULT '',
  ADD COLUMN bucket_name text NOT NULL DEFAULT '',
  ADD COLUMN bucket_folder text NOT NULL DEFAULT '',
  ADD COLUMN bucket_region text NOT NULL DEFAULT '',
  ADD COLUMN bucket_access_key text NOT NULL DEFAULT '',
  ADD COLUMN bucket_secret_key text NOT NULL DEFAULT '',
  ADD CONSTRAINT storage_bucket CHECK (storage = 'disk' OR bucket_name <> '');

-- +goose Down
ALTER TABLE server
  DROP COLUMN storage, DROP COLUMN bucket_endpoint, DROP COLUMN bucket_name, DROP COLUMN bucket_folder,
  DROP COLUMN bucket_region, DROP COLUMN bucket_access_key, DROP COLUMN bucket_secret_key;
