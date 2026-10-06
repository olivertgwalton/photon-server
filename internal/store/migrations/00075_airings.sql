-- +goose Up
-- Each provider's next episode to air of a show, which may have no file yet, as Jellyfin lists
-- episodes yet to premiere. A source that knows of none has no row.
CREATE TABLE next_airings (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL CONSTRAINT airing_source CHECK (source = 'tmdb' OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  season_number int NOT NULL,
  episode_number int NOT NULL,
  title text NOT NULL,
  air_date date NOT NULL,
  PRIMARY KEY (item_id, source)
);

-- +goose Down
DROP TABLE next_airings;
