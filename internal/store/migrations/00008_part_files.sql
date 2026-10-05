-- +goose Up
-- A part's bytes can sit at several paths: a byte-identical copy in a second folder is another
-- place to read the same version, not a second version.
CREATE TABLE part_files (
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  rel_path text NOT NULL,
  size_bytes bigint NOT NULL,
  mtime_ns bigint NOT NULL,
  PRIMARY KEY (library_id, rel_path)
);
CREATE INDEX part_files_part ON part_files (part_id);
INSERT INTO part_files (part_id, library_id, rel_path, size_bytes, mtime_ns)
  SELECT id, library_id, rel_path, size_bytes, mtime_ns FROM parts;
ALTER TABLE parts DROP COLUMN library_id, DROP COLUMN rel_path, DROP COLUMN mtime_ns;

-- +goose Down
ALTER TABLE parts ADD COLUMN library_id uuid REFERENCES libraries(id) ON DELETE CASCADE,
  ADD COLUMN rel_path text, ADD COLUMN mtime_ns bigint;
UPDATE parts p SET library_id = f.library_id, rel_path = f.rel_path, mtime_ns = f.mtime_ns
  FROM (SELECT DISTINCT ON (part_id) * FROM part_files ORDER BY part_id, rel_path) f WHERE f.part_id = p.id;
DROP TABLE part_files;
ALTER TABLE parts ALTER COLUMN library_id SET NOT NULL, ALTER COLUMN rel_path SET NOT NULL,
  ALTER COLUMN mtime_ns SET NOT NULL, ADD UNIQUE (library_id, rel_path);
