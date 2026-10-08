-- +goose NO TRANSACTION
-- +goose Up
-- A member and a restricted profile were the same profile under two names: what either sees is
-- what its access allows. Both are a user.
UPDATE profiles SET role = 'user' WHERE role IN ('member', 'restricted');
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profile_role,
  ADD CONSTRAINT profile_role CHECK (role IN ('admin', 'user')) NOT VALID;
ALTER TABLE profiles VALIDATE CONSTRAINT profile_role;

-- +goose Down
UPDATE profiles SET role = 'member' WHERE role = 'user';
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profile_role,
  ADD CONSTRAINT profile_role CHECK (role IN ('admin', 'member', 'restricted')) NOT VALID;
ALTER TABLE profiles VALIDATE CONSTRAINT profile_role;
