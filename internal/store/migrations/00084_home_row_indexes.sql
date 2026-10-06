-- +goose NO TRANSACTION
-- +goose Up
-- The episodes released lately and the best IMDb ratings, for home's recently released and top
-- rated rows, read from an index rather than every episode and every rating.
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_episode_released ON items ((coalesce(release_date, air_date)))
  WHERE kind = 'episode';
CREATE INDEX CONCURRENTLY IF NOT EXISTS ratings_site_score ON ratings (site, score DESC, item_id DESC) INCLUDE (votes);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS ratings_site_score;
DROP INDEX CONCURRENTLY IF EXISTS items_episode_released;
