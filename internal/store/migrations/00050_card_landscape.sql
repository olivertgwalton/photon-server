-- +goose Up
-- A profile's Continue Watching and Next Up may draw a card by the landscape picture lettered with
-- its show's or film's name. One row per profile, so nothing here waits on a large table.
ALTER TABLE profile_preferences DROP CONSTRAINT card_art,
  ADD CONSTRAINT card_art CHECK (card_art IN ('still', 'backdrop', 'landscape'));

-- +goose Down
UPDATE profile_preferences SET card_art = 'backdrop' WHERE card_art = 'landscape';
ALTER TABLE profile_preferences DROP CONSTRAINT card_art,
  ADD CONSTRAINT card_art CHECK (card_art IN ('still', 'backdrop'));
