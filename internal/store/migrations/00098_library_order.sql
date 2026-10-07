-- +goose Up
-- Each profile's libraries in its own order, as Plex's sidebar and Jellyfin's ordered views keep a
-- user's. Those it has not placed follow, by name.
CREATE TABLE library_order (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  position smallint NOT NULL,
  PRIMARY KEY (profile_id, library_id)
);
CREATE INDEX library_order_library ON library_order (library_id);

-- +goose Down
DROP TABLE library_order;
