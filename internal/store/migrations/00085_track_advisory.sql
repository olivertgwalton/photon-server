-- +goose NO TRANSACTION
-- +goose Up
-- Whether a track is explicit or clean, as its tags mark it; NULL where they say neither. A column
-- with no default rewrites nothing, its check is checked apart from its adding, reading items
-- without stopping its writes, and a music library's folders are forgotten so its next scan reads
-- its tracks' tags again.
ALTER TABLE items ADD COLUMN IF NOT EXISTS advisory text;
ALTER TABLE items ADD CONSTRAINT item_advisory CHECK (advisory IN ('explicit', 'clean')) NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT item_advisory;
DELETE FROM folders WHERE library_id IN (SELECT id FROM libraries WHERE kind = 'music');

-- +goose Down
ALTER TABLE items DROP COLUMN IF EXISTS advisory;
