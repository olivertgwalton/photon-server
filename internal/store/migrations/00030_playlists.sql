-- +goose Up
-- A profile's playlists: its own, in its order. An entry has an id of its own, so one title may be
-- in a playlist twice and each is moved or removed by itself, as Jellyfin's PlaylistItemId is.
CREATE TABLE playlists (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  name text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX playlists_profile ON playlists (profile_id, name);

CREATE TABLE playlist_entries (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  playlist_id uuid NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  position int NOT NULL
);
CREATE INDEX playlist_entries_order ON playlist_entries (playlist_id, position);

-- +goose Down
DROP TABLE playlist_entries, playlists;
