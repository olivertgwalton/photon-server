-- +goose Up
-- Whether the server also answers Jellyfin's API, for the apps made for Jellyfin, and the port it
-- does so on: Jellyfin's own by default, where those apps look first.
ALTER TABLE server
  ADD COLUMN jellyfin text NOT NULL DEFAULT 'off' CONSTRAINT jellyfin CHECK (jellyfin IN ('on', 'off')),
  ADD COLUMN jellyfin_port integer NOT NULL DEFAULT 8096
    CONSTRAINT jellyfin_port CHECK (jellyfin_port BETWEEN 1 AND 65535);

-- +goose Down
ALTER TABLE server DROP COLUMN jellyfin, DROP COLUMN jellyfin_port;
