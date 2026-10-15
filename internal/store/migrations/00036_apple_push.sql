-- +goose Up
-- An Apple device is pushed to as a browser is, through photon's push relay to APNs: its token is
-- its endpoint, and it has the APNs environment that issued the token in place of a browser's keys.
-- The relay knows a server by the credential it gave it.
ALTER TABLE push_subscriptions
  ADD COLUMN kind text NOT NULL DEFAULT 'web' CONSTRAINT push_kind CHECK (kind IN ('web', 'apple')),
  ADD COLUMN apns_environment text
    CONSTRAINT apns_environment CHECK (apns_environment IN ('production', 'development')),
  ALTER COLUMN p256dh DROP NOT NULL,
  ALTER COLUMN auth DROP NOT NULL,
  ADD CONSTRAINT push_subscription_keys CHECK (CASE kind
    WHEN 'web' THEN p256dh IS NOT NULL AND auth IS NOT NULL AND apns_environment IS NULL
    ELSE p256dh IS NULL AND auth IS NULL AND apns_environment IS NOT NULL END);
ALTER TABLE server ADD COLUMN push_relay_credential text;

-- +goose Down
ALTER TABLE server DROP COLUMN push_relay_credential;
DELETE FROM push_subscriptions WHERE kind = 'apple';
ALTER TABLE push_subscriptions DROP CONSTRAINT push_subscription_keys,
  DROP COLUMN apns_environment, DROP COLUMN kind,
  ALTER COLUMN p256dh SET NOT NULL, ALTER COLUMN auth SET NOT NULL;
