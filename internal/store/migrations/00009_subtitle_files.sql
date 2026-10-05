-- +goose Up
CREATE TABLE subtitle_files (
  version_id uuid NOT NULL REFERENCES versions(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  rel_path text NOT NULL,
  codec text NOT NULL,
  language text,
  title text,
  forced bool NOT NULL,
  is_default bool NOT NULL,
  hearing_impaired bool NOT NULL,
  size_bytes bigint NOT NULL,
  mtime_ns bigint NOT NULL,
  PRIMARY KEY (library_id, rel_path)
);
CREATE INDEX subtitle_files_version ON subtitle_files (version_id);

-- +goose Down
DROP TABLE subtitle_files;
