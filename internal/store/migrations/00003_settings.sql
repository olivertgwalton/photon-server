-- +goose Up
-- Where the other nodes reach a node, as an admin sets it; none is a node handed no one else's
-- requests.
ALTER TABLE node ADD COLUMN address text NOT NULL DEFAULT '';

-- How clients reach the server, as an admin sets it: its address outside, the proxies trusted to
-- say who a client is, and whether it answers clients looking for it.
ALTER TABLE server
  ADD COLUMN public_url text NOT NULL DEFAULT '',
  ADD COLUMN trusted_proxies cidr[] NOT NULL DEFAULT '{}',
  ADD COLUMN discovery text NOT NULL DEFAULT 'broadcast'
    CONSTRAINT discovery CHECK (discovery IN ('broadcast', 'off'));

-- What the server is called, none being each node's host's, and the language its metadata is
-- asked in, as an admin sets them, beside the country it takes certificates from.
ALTER TABLE server
  ADD COLUMN name text NOT NULL DEFAULT '',
  ADD COLUMN metadata_language text NOT NULL DEFAULT 'en-US';
-- A new server asks in en-US, and so for the United States' certificates.
UPDATE server SET certificate_country = 'US';

-- +goose Down
UPDATE server SET certificate_country = NULL;
ALTER TABLE server DROP COLUMN name, DROP COLUMN metadata_language;
ALTER TABLE server DROP COLUMN public_url, DROP COLUMN trusted_proxies, DROP COLUMN discovery;
ALTER TABLE node DROP COLUMN address;
