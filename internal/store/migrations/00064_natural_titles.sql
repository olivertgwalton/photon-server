-- +goose Up
-- Titles sort with their numbers read as numbers, as Jellyfin's do: 2 Fast 2 Furious, 13 Going on
-- 30, 21 Jump Street, 1917. ICU's numeric ordering does it in the column itself, so every order by
-- title and its index follow; the index is rebuilt, the rows are not rewritten.
CREATE COLLATION title_order (provider = icu, locale = 'und-u-kn');
ALTER TABLE items ALTER COLUMN sort_title TYPE text COLLATE title_order;

-- +goose Down
ALTER TABLE items ALTER COLUMN sort_title TYPE text COLLATE "default";
DROP COLLATION title_order;
