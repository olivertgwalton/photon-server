-- +goose Up
-- Each profile's home rows in its own order, shown or hidden, as Jellyfin keeps a user's home
-- sections. A profile with none has every row, in the server's order.
CREATE TABLE home_sections (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  home_row text NOT NULL CONSTRAINT home_row CHECK (home_row IN ('continue_watching', 'next_up', 'favourites', 'recently_added_films', 'recently_added_shows')),
  position smallint NOT NULL,
  visibility text NOT NULL CONSTRAINT row_visibility CHECK (visibility IN ('shown', 'hidden')),
  PRIMARY KEY (profile_id, home_row)
);

-- +goose Down
DROP TABLE home_sections;
