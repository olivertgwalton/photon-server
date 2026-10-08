-- +goose Up
-- A shows folder holding one copy under several names is read again at the next scan, so an
-- episode riven shows under each of its numbers, kept as one episode under the last of them,
-- becomes an episode for each.
DELETE FROM folders f USING part_files p, libraries l
WHERE l.id = f.library_id AND l.kind = 'shows' AND p.library_id = f.library_id
	AND f.path = regexp_replace(p.rel_path, '/[^/]*$', '')
	AND p.part_id IN (SELECT part_id FROM part_files GROUP BY part_id HAVING count(*) > 1);

-- +goose Down
-- Nothing is lost: the next scan remembers each folder it reads.
SELECT 1;
