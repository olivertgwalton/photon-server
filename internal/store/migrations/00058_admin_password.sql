-- +goose NO TRANSACTION
-- +goose Up
-- An admin signs in with a password, whatever writes the profile.
ALTER TABLE profiles ADD CONSTRAINT admin_password CHECK (role <> 'admin' OR password_hash IS NOT NULL) NOT VALID;
ALTER TABLE profiles VALIDATE CONSTRAINT admin_password;

-- +goose Down
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS admin_password;
