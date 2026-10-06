-- +goose Up
-- Seasons a provider renamed take back the name their number gives them.
UPDATE items i SET title = i.scan_title, sort_title = lower(i.scan_title)
FROM item_fields f
WHERE f.item_id = i.id AND f.field = 'title' AND i.kind = 'season'
  AND f.source NOT IN ('file', 'nfo', 'user');
UPDATE item_fields f SET source = 'file'
FROM items i
WHERE f.item_id = i.id AND i.kind = 'season' AND f.field IN ('title', 'sort_title')
  AND f.source NOT IN ('file', 'nfo', 'user');

-- +goose Down
-- The providers' names come back at the next refresh.
SELECT 1;
