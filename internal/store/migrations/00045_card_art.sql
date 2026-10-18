-- +goose Up
-- What a profile's Continue Watching and Next Up draw a card by: its own still, as they have, or
-- the backdrop of the show it is of. One row per profile, so nothing here waits on a large table.
ALTER TABLE profile_preferences ADD COLUMN card_art text NOT NULL DEFAULT 'still'
  CONSTRAINT card_art CHECK (card_art IN ('still', 'backdrop'));

-- +goose Down
ALTER TABLE profile_preferences DROP COLUMN card_art;
