-- +goose Up
-- A profile that never chose draws Continue Watching and Next Up by its show's or film's lettered
-- landscape picture. A profile that kept a preference keeps the card art it was kept with. Only the
-- column's default changes, which rewrites nothing.
ALTER TABLE profile_preferences ALTER COLUMN card_art SET DEFAULT 'landscape';

-- +goose Down
ALTER TABLE profile_preferences ALTER COLUMN card_art SET DEFAULT 'still';
