-- +goose Up
CREATE TABLE library_sources (
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  source text NOT NULL CONSTRAINT library_source CHECK (source IN ('nfo', 'tmdb')),
  position smallint NOT NULL,
  PRIMARY KEY (library_id, source),
  UNIQUE (library_id, position)
);
INSERT INTO library_sources (library_id, source, position)
  SELECT id, s, p FROM libraries, (VALUES ('nfo', 0), ('tmdb', 1)) v(s, p);

-- +goose Down
DROP TABLE library_sources;
