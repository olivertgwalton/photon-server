-- +goose Up
-- Every profile signs in with its password, as each Jellyfin user does. profiles is a household's
-- few rows, so setting NOT NULL holds nothing for long.
ALTER TABLE profiles DROP CONSTRAINT admin_password;
ALTER TABLE profiles ALTER COLUMN password_hash SET NOT NULL;

-- +goose Down
ALTER TABLE profiles ALTER COLUMN password_hash DROP NOT NULL;
ALTER TABLE profiles ADD CONSTRAINT admin_password CHECK (role <> 'admin' OR password_hash IS NOT NULL);
