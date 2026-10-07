-- +goose Up
-- Whether the server's port answers HTTPS, as Plex's Secure connections, and the certificate it
-- serves: a PEM chain and its key, at paths each node reads.
ALTER TABLE server
  ADD COLUMN secure_connections text NOT NULL DEFAULT 'disabled'
    CONSTRAINT secure_connections CHECK (secure_connections IN ('required', 'preferred', 'disabled')),
  ADD COLUMN tls_certificate text,
  ADD COLUMN tls_key text,
  ADD CONSTRAINT secure_certificate
    CHECK (secure_connections = 'disabled' OR (tls_certificate IS NOT NULL AND tls_key IS NOT NULL));

-- +goose Down
ALTER TABLE server DROP COLUMN secure_connections, DROP COLUMN tls_certificate, DROP COLUMN tls_key;
