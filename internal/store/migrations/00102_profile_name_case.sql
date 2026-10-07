-- +goose NO TRANSACTION
-- +goose Up
-- A profile's name is one whatever its case, as Jellyfin's user names: "kid" signs in as "Kid", and
-- cannot be added beside it. The case-sensitive constraint is then redundant.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS profiles_name_lower ON profiles (lower(name));
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profiles_name_key;

-- +goose Down
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS profiles_name_key ON profiles (name);
ALTER TABLE profiles ADD CONSTRAINT profiles_name_key UNIQUE USING INDEX profiles_name_key;
DROP INDEX CONCURRENTLY IF EXISTS profiles_name_lower;
