-- +goose Up
-- An extra's previews made before a part with no chapters was pictured as one are made again, so
-- its card has a still.
INSERT INTO jobs (kind, subject)
SELECT 'previews', pv.part_id FROM previews pv JOIN parts p ON p.id = pv.part_id
JOIN versions v ON v.id = p.version_id JOIN items i ON i.id = v.item_id
WHERE i.kind = 'extra' AND cardinality(pv.chapter_images) = 0
ON CONFLICT (kind, subject) DO NOTHING;

-- +goose Down
-- The pictures made stand: a still is harmless under the old code.
