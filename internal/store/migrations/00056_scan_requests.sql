-- +goose Up
-- The folders a library's next scan is asked to read, '.' for all of it. A scan reads what was
-- asked when it started, and forgets only what has not been asked again since.
CREATE TABLE scan_requests (
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  path text NOT NULL,
  asked_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (library_id, path)
);

-- +goose Down
DROP TABLE scan_requests;
