-- +goose Up
-- The OpenID Connect providers an admin registered the server on as a client, which the household
-- signs in through. The slug is in the address the provider sends a browser back to. A provider
-- that makes profiles gives each what max_age, unrated and its libraries say, as a profile's own
-- access does.
CREATE TABLE sign_in_providers (
  slug text PRIMARY KEY CONSTRAINT sign_in_provider_slug CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
  name text NOT NULL,
  issuer text NOT NULL,
  client_id text NOT NULL,
  client_secret text NOT NULL,
  provisioning text NOT NULL CONSTRAINT provisioning CHECK (provisioning IN ('link', 'create')),
  required_group text NOT NULL DEFAULT '',
  recheck text NOT NULL CONSTRAINT recheck CHECK (recheck IN ('hourly', 'at_sign_in')),
  max_age smallint CHECK (max_age >= 0),
  unrated text NOT NULL DEFAULT 'allow' CONSTRAINT sign_in_provider_unrated CHECK (unrated IN ('allow', 'block')),
  -- Only those it lets in get a profile made.
  CONSTRAINT sign_in_provider_creates_for_a_group CHECK (provisioning <> 'create' OR required_group <> '')
);

CREATE TABLE sign_in_provider_libraries (
  provider text NOT NULL REFERENCES sign_in_providers(slug) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  PRIMARY KEY (provider, library_id)
);
CREATE INDEX sign_in_provider_libraries_library ON sign_in_provider_libraries (library_id);

-- The account a profile linked at a provider: its subject, which the provider never gives another
-- account, and what the provider called it then. A provider that rechecks its accounts is asked by
-- the refresh token it granted last, an hour after checked_at.
CREATE TABLE sign_in_accounts (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  provider text NOT NULL REFERENCES sign_in_providers(slug) ON DELETE CASCADE,
  subject text NOT NULL,
  username text NOT NULL,
  linked_at timestamptz NOT NULL DEFAULT now(),
  refresh_token text,
  checked_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (profile_id, provider),
  CONSTRAINT sign_in_account UNIQUE (provider, subject)
);

-- A profile a provider's account was given has no password until it sets one.
ALTER TABLE profiles ALTER COLUMN password_hash DROP NOT NULL;

-- The account a device's session was signed in by, or whose session approved its pairing: the
-- session goes with the account, so unlinking it, or the provider, signs the device out. Both
-- constraints are checked of the sessions there are in 00007, without holding the table.
ALTER TABLE device_sessions ADD COLUMN sign_in_provider text, ADD COLUMN sign_in_subject text,
  ADD CONSTRAINT device_session_sign_in FOREIGN KEY (sign_in_provider, sign_in_subject)
    REFERENCES sign_in_accounts (provider, subject) ON DELETE CASCADE NOT VALID,
  ADD CONSTRAINT device_session_sign_in_whole
    CHECK ((sign_in_provider IS NULL) = (sign_in_subject IS NULL)) NOT VALID;

-- +goose Down
ALTER TABLE device_sessions DROP COLUMN sign_in_subject, DROP COLUMN sign_in_provider;
ALTER TABLE profiles ALTER COLUMN password_hash SET NOT NULL;
DROP TABLE sign_in_accounts;
DROP TABLE sign_in_provider_libraries;
DROP TABLE sign_in_providers;
