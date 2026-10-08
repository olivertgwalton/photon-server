-- +goose NO TRANSACTION
-- +goose Up
-- A manager keeps the profiles it adds: managed_by is the manager, and null for one the admin
-- keeps, which is what a removed manager's profiles become.
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS managed_by uuid;
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profiles_managed_by_fkey,
  ADD CONSTRAINT profiles_managed_by_fkey FOREIGN KEY (managed_by) REFERENCES profiles(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE profiles VALIDATE CONSTRAINT profiles_managed_by_fkey;
CREATE INDEX CONCURRENTLY IF NOT EXISTS profiles_managed_by ON profiles (managed_by);
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profile_role,
  ADD CONSTRAINT profile_role CHECK (role IN ('admin', 'manager', 'user')) NOT VALID;
ALTER TABLE profiles VALIDATE CONSTRAINT profile_role;

-- +goose Down
UPDATE profiles SET role = 'user' WHERE role = 'manager';
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profile_role,
  ADD CONSTRAINT profile_role CHECK (role IN ('admin', 'user')) NOT VALID;
ALTER TABLE profiles VALIDATE CONSTRAINT profile_role;
DROP INDEX CONCURRENTLY IF EXISTS profiles_managed_by;
ALTER TABLE profiles DROP COLUMN IF EXISTS managed_by;
