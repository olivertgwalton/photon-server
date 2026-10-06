-- +goose Up
-- A profile's own picture, as Jellyfin's user image: kept by the server under this id and served as
-- any picture is.
ALTER TABLE profiles ADD COLUMN avatar_id uuid;

-- +goose Down
ALTER TABLE profiles DROP COLUMN avatar_id;
