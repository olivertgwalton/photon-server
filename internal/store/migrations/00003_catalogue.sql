-- +goose Up
CREATE TABLE folders (
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  path text NOT NULL,
  fingerprint bytea NOT NULL,
  PRIMARY KEY (library_id, path)
);

CREATE TABLE items (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  kind text NOT NULL CONSTRAINT item_kind CHECK (kind IN ('movie')),
  title text NOT NULL,
  sort_title text NOT NULL,
  year int,
  folder text NOT NULL,
  added_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX items_library_sort ON items (library_id, kind, sort_title, id);

CREATE TABLE external_ids (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  provider text NOT NULL CONSTRAINT id_provider CHECK (provider IN ('tmdb', 'imdb', 'tvdb')),
  value text NOT NULL,
  source text NOT NULL CONSTRAINT id_source CHECK (source IN ('path')),
  PRIMARY KEY (item_id, provider)
);

CREATE TABLE versions (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  fingerprint bytea NOT NULL,
  edition text,
  label text,
  container text NOT NULL,
  width int,
  height int,
  video_codec text,
  video_range text CONSTRAINT video_range CHECK (video_range IN ('sdr', 'hlg', 'hdr10', 'hdr10plus', 'dv')),
  dv_profile smallint,
  bitrate_kbps int NOT NULL,
  size_bytes bigint NOT NULL,
  duration_ms bigint NOT NULL,
  missing_since timestamptz,
  UNIQUE (library_id, fingerprint)
);

CREATE TABLE parts (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  version_id uuid NOT NULL REFERENCES versions(id) ON DELETE CASCADE,
  idx smallint NOT NULL,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  rel_path text NOT NULL,
  size_bytes bigint NOT NULL,
  mtime_ns bigint NOT NULL,
  duration_ms bigint NOT NULL,
  offset_ms bigint NOT NULL,
  UNIQUE (version_id, idx),
  UNIQUE (library_id, rel_path)
);

CREATE TABLE streams (
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  idx int NOT NULL,
  kind text NOT NULL CONSTRAINT stream_kind CHECK (kind IN ('video', 'audio', 'subtitle')),
  codec text NOT NULL,
  profile text,
  language text,
  title text,
  is_default bool NOT NULL,
  forced bool NOT NULL,
  hearing_impaired bool NOT NULL,
  commentary bool NOT NULL,
  width int,
  height int,
  frame_rate double precision,
  video_range text CONSTRAINT stream_video_range CHECK (video_range IN ('sdr', 'hlg', 'hdr10', 'hdr10plus', 'dv')),
  dv_profile smallint,
  dv_level smallint,
  dv_compatibility smallint,
  channels int,
  channel_layout text,
  sample_rate int,
  bitrate_kbps int,
  PRIMARY KEY (part_id, idx)
);

CREATE TABLE chapters (
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  idx int NOT NULL,
  start_ms bigint NOT NULL,
  end_ms bigint NOT NULL,
  title text,
  PRIMARY KEY (part_id, idx)
);

-- +goose Down
DROP TABLE chapters, streams, parts, versions, external_ids, items, folders;
