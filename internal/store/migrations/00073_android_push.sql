-- +goose NO TRANSACTION
-- +goose Up
-- An Android device is pushed to as an Apple device is, through photon's push relay, to Firebase
-- Cloud Messaging: its registration token is its endpoint, and it has neither a browser's keys nor
-- an APNs environment. Each check is checked again apart from its change, reading the table
-- without stopping its writes.
ALTER TABLE push_subscriptions DROP CONSTRAINT push_kind,
  ADD CONSTRAINT push_kind CHECK (kind IN ('web', 'apple', 'android')) NOT VALID;
ALTER TABLE push_subscriptions VALIDATE CONSTRAINT push_kind;
ALTER TABLE push_subscriptions DROP CONSTRAINT push_subscription_keys,
  ADD CONSTRAINT push_subscription_keys CHECK (CASE kind
    WHEN 'web' THEN p256dh IS NOT NULL AND auth IS NOT NULL AND apns_environment IS NULL
    WHEN 'apple' THEN p256dh IS NULL AND auth IS NULL AND apns_environment IS NOT NULL
    ELSE p256dh IS NULL AND auth IS NULL AND apns_environment IS NULL END) NOT VALID;
ALTER TABLE push_subscriptions VALIDATE CONSTRAINT push_subscription_keys;

-- +goose Down
DELETE FROM push_subscriptions WHERE kind = 'android';
ALTER TABLE push_subscriptions DROP CONSTRAINT push_subscription_keys,
  ADD CONSTRAINT push_subscription_keys CHECK (CASE kind
    WHEN 'web' THEN p256dh IS NOT NULL AND auth IS NOT NULL AND apns_environment IS NULL
    ELSE p256dh IS NULL AND auth IS NULL AND apns_environment IS NOT NULL END) NOT VALID;
ALTER TABLE push_subscriptions VALIDATE CONSTRAINT push_subscription_keys;
ALTER TABLE push_subscriptions DROP CONSTRAINT push_kind,
  ADD CONSTRAINT push_kind CHECK (kind IN ('web', 'apple')) NOT VALID;
ALTER TABLE push_subscriptions VALIDATE CONSTRAINT push_kind;
