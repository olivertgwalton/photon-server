-- +goose Up
-- Which clients are remote, and the most a stream to one is sent at, as Jellyfin's LAN networks
-- and Internet streaming bitrate limit. No networks is this machine's and the private ones; a
-- limit of 0 is none.
ALTER TABLE server
  ADD COLUMN local_networks cidr[] NOT NULL DEFAULT '{}',
  ADD COLUMN remote_max_bitrate_kbps integer NOT NULL DEFAULT 0
    CONSTRAINT remote_max_bitrate_kbps CHECK (remote_max_bitrate_kbps >= 0);

-- +goose Down
ALTER TABLE server DROP COLUMN local_networks, DROP COLUMN remote_max_bitrate_kbps;
