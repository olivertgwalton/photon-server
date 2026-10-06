-- +goose Up
-- Metacritic, Letterboxd and Trakt are no longer kept: no client draws them.
DELETE FROM ratings WHERE site IN ('metacritic', 'letterboxd', 'trakt');
ALTER TABLE ratings DROP CONSTRAINT rating_site,
  ADD CONSTRAINT rating_site CHECK (site IN ('imdb', 'tmdb', 'rotten_tomatoes', 'rotten_tomatoes_audience'));

-- +goose Down
ALTER TABLE ratings DROP CONSTRAINT rating_site,
  ADD CONSTRAINT rating_site CHECK (site IN ('imdb', 'tmdb', 'rotten_tomatoes',
    'rotten_tomatoes_audience', 'metacritic', 'letterboxd', 'trakt'));
