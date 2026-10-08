-- +goose Up
-- The episodes a provider lists in a season of a show, with a file or not, aired, dated or not: the
-- calendar shows those with no file beside those that are here, as Jellyfin's missing and unaired
-- episodes, and knows from them which is a season's last. Each season is what the highest ranking
-- source to describe it last said of it. An episode with no file is known to clients by id, the
-- same for as long as it is listed.
CREATE TABLE announced_episodes (
  show_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  season_number integer NOT NULL,
  episode_number integer NOT NULL,
  id uuid NOT NULL UNIQUE GENERATED ALWAYS AS (
    md5(show_id::text || '/' || season_number || '/' || episode_number)::uuid) STORED,
  source text NOT NULL CONSTRAINT announced_source CHECK (
    source IN ('tmdb', 'tvdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  title text NOT NULL,
  overview text NOT NULL,
  air_date date,
  PRIMARY KEY (show_id, season_number, episode_number)
);
CREATE INDEX announced_episodes_air_date ON announced_episodes (air_date);

-- +goose Down
DROP TABLE announced_episodes;
