-- +goose Up
-- How clients are given what is kept in a bucket: through the server, or sent to read it from the
-- bucket, at the address they reach it at where that is not the server's.
ALTER TABLE server
  ADD COLUMN bucket_delivery text NOT NULL DEFAULT 'proxy'
    CONSTRAINT bucket_delivery CHECK (bucket_delivery IN ('proxy', 'redirect')),
  ADD COLUMN bucket_public_endpoint text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE server DROP COLUMN bucket_delivery, DROP COLUMN bucket_public_endpoint;
