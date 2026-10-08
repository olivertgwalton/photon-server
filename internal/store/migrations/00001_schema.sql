-- +goose Up
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Titles sort with their numbers read as numbers, as Jellyfin's do: 2 Fast 2 Furious, 13 Going on
-- 30, 21 Jump Street, 1917.
CREATE COLLATION title_order (provider = icu, locale = 'und-u-kn');

-- unaccent is only stable, as its rules could change; a title's search words are stored, so this
-- fixes them to the rules the extension has now. A new extension version means running
-- UPDATE items SET title = title.
CREATE FUNCTION search_text(t text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
  RETURN lower(public.unaccent('public.unaccent'::regdictionary, t));

CREATE TABLE libraries (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  name text NOT NULL UNIQUE,
  kind text NOT NULL CONSTRAINT library_kind CHECK (kind IN ('movies', 'shows')),
  root text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now(),
  monitor text NOT NULL DEFAULT 'realtime'
    CONSTRAINT monitor CHECK (monitor IN ('realtime', 'off')),
  refresh_days smallint NOT NULL DEFAULT 30 CHECK (refresh_days BETWEEN 0 AND 365),
  previews text NOT NULL DEFAULT 'all'
    CONSTRAINT preview_level CHECK (previews IN ('off', 'chapters', 'all')),
  markers text NOT NULL DEFAULT 'all'
    CONSTRAINT marker_detection CHECK (markers IN ('off', 'chapters', 'all')),
  keyframes text NOT NULL DEFAULT 'index'
    CONSTRAINT keyframe_mode CHECK (keyframes IN ('index', 'full', 'off')),
  themes text NOT NULL DEFAULT 'local'
    CONSTRAINT theme_lookup CHECK (themes IN ('local', 'themerr', 'off')),
  deletion text NOT NULL DEFAULT 'off'
    CONSTRAINT media_deletion CHECK (deletion IN ('off', 'files')),
  metadata_language text,
  certification_country text
    CONSTRAINT certification_country CHECK (certification_country ~ '^[A-Z]{2}$'),
  artwork_language text NOT NULL DEFAULT 'localized'
    CONSTRAINT artwork_language CHECK (artwork_language IN ('localized', 'any')),
  title_language text NOT NULL DEFAULT 'localized'
    CONSTRAINT title_language CHECK (title_language IN ('localized', 'original')),
  collection_mode text NOT NULL DEFAULT 'grouped'
    CONSTRAINT collection_mode CHECK (collection_mode IN ('grouped', 'shown', 'hidden')),
  subtitle_languages text[] NOT NULL DEFAULT '{}',
  subtitle_match text NOT NULL DEFAULT 'release'
    CONSTRAINT subtitle_match CHECK (subtitle_match IN ('release', 'any'))
);

CREATE TABLE items (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  kind text NOT NULL
    CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season', 'episode', 'extra',
      'collection')),
  title text NOT NULL,
  sort_title text COLLATE title_order NOT NULL,
  year int,
  folder text NOT NULL,
  added_at timestamptz NOT NULL DEFAULT now(),
  parent_id uuid REFERENCES items(id) ON DELETE CASCADE,
  season_number int,
  episode_number int,
  episode_end int,
  air_date date,
  extra_kind text
    CONSTRAINT extra_kind CHECK (extra_kind IN ('trailer', 'teaser', 'featurette',
      'behind_the_scenes', 'deleted_scene', 'interview', 'scene', 'short', 'clip', 'blooper',
      'theme_video', 'other')),
  scan_title text NOT NULL,
  original_title text,
  overview text,
  tagline text,
  certificate text,
  release_date date,
  genres jsonb,
  studios jsonb,
  released_asc date
    GENERATED ALWAYS AS (coalesce(release_date, make_date(year, 1, 1),
      '9999-12-31')) STORED NOT NULL,
  released_desc date
    GENERATED ALWAYS AS (coalesce(release_date, make_date(year, 1, 1),
      '0001-01-01')) STORED NOT NULL,
  search tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', search_text(title || ' ' || coalesce(original_title,
      '')))) STORED NOT NULL,
  identified_at timestamptz,
  episode_order text NOT NULL DEFAULT 'aired'
    CONSTRAINT episode_order CHECK (episode_order IN ('aired', 'dvd', 'absolute')),
  unmatched_at timestamptz,
  metadata_language text,
  certification_country text
    CONSTRAINT item_certification_country CHECK (certification_country ~ '^[A-Z]{2}$'),
  CONSTRAINT extra_has_kind CHECK ((kind = 'extra') = (extra_kind IS NOT NULL))
);
CREATE INDEX items_added ON items (kind, added_at DESC, id DESC);
CREATE INDEX items_certificate ON items (certificate) WHERE certificate IS NOT NULL;
CREATE INDEX items_episode_released ON items (coalesce(release_date,
  air_date)) WHERE kind = 'episode';
CREATE INDEX items_genres ON items USING gin (genres);
CREATE INDEX items_library_added ON items (library_id, kind, added_at, id);
CREATE INDEX items_library_folder ON items (library_id, folder);
CREATE INDEX items_library_released_asc ON items (library_id, kind, released_asc, id);
CREATE INDEX items_library_released_desc ON items (library_id, kind, released_desc, id);
CREATE INDEX items_library_sort ON items (library_id, kind, sort_title, id);
CREATE INDEX items_parent ON items (parent_id, season_number, episode_number);
CREATE INDEX items_search ON items USING gin (search);
CREATE INDEX items_studios ON items USING gin (studios);
CREATE INDEX items_trigrams ON items USING gin
  (search_text(title || ' ' || coalesce(original_title, '')) gin_trgm_ops);

CREATE TABLE profiles (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  name text NOT NULL,
  role text NOT NULL CONSTRAINT profile_role CHECK (role IN ('admin', 'manager', 'user')),
  password_hash text NOT NULL,
  pin_hash text,
  created_at timestamptz NOT NULL DEFAULT now(),
  max_age smallint CHECK (max_age >= 0),
  unrated text NOT NULL DEFAULT 'allow'
    CONSTRAINT profile_unrated CHECK (unrated IN ('allow', 'block')),
  avatar_id uuid,
  managed_by uuid REFERENCES profiles(id) ON DELETE SET NULL
);
CREATE INDEX profiles_managed_by ON profiles (managed_by);
CREATE UNIQUE INDEX profiles_name_lower ON profiles (lower(name));

CREATE TABLE activity (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  at timestamptz NOT NULL DEFAULT now(),
  kind text NOT NULL
    CONSTRAINT activity_kind CHECK (kind IN ('playback.started', 'playback.stopped',
      'auth.signed_in', 'auth.sign_in_refused', 'profile.added', 'profile.removed',
      'library.added', 'library.removed', 'library.scanned', 'library.titles_added', 'task.failed',
      'backup.made', 'job.dead')),
  profile_id uuid REFERENCES profiles(id) ON DELETE SET NULL,
  item_id uuid REFERENCES items(id) ON DELETE SET NULL,
  library_id uuid REFERENCES libraries(id) ON DELETE SET NULL,
  details jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX activity_at ON activity (at DESC, id DESC);
CREATE INDEX activity_item ON activity (item_id);
CREATE INDEX activity_kind_at ON activity (kind, at DESC, id DESC);
CREATE INDEX activity_library ON activity (library_id);
CREATE INDEX activity_profile ON activity (profile_id);

CREATE TABLE announced_episodes (
  show_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  season_number int NOT NULL,
  episode_number int NOT NULL,
  id uuid
    GENERATED ALWAYS AS (md5(show_id::text || '/' || season_number || '/' || episode_number)::uuid)
    STORED NOT NULL UNIQUE,
  source text NOT NULL
    CONSTRAINT announced_source CHECK (source IN ('tmdb',
      'tvdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  title text NOT NULL,
  overview text NOT NULL,
  air_date date,
  PRIMARY KEY (show_id, season_number, episode_number)
);
CREATE INDEX announced_episodes_air_date ON announced_episodes (air_date);

CREATE TABLE artwork (
  id uuid NOT NULL DEFAULT uuidv7() UNIQUE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL
    CONSTRAINT artwork_source CHECK (source IN ('file', 'tmdb', 'tvdb', 'user',
      'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  kind text NOT NULL
    CONSTRAINT artwork_kind CHECK (kind IN ('poster', 'backdrop', 'logo', 'thumb', 'banner')),
  place text NOT NULL,
  position smallint NOT NULL,
  folder text,
  language text,
  width int,
  height int,
  blurhash text,
  PRIMARY KEY (item_id, source, kind, place),
  CONSTRAINT artwork_file_has_folder CHECK ((source = 'file') = (folder IS NOT NULL))
);
CREATE UNIQUE INDEX artwork_chosen ON artwork (item_id, kind) WHERE source = 'user';

CREATE TABLE certificates (
  country text NOT NULL,
  rating text NOT NULL,
  age smallint NOT NULL,
  PRIMARY KEY (country, rating)
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
  video_range text
    CONSTRAINT video_range CHECK (video_range IN ('sdr', 'hlg', 'hdr10', 'hdr10plus', 'dv')),
  dv_profile smallint,
  bitrate_kbps int NOT NULL,
  size_bytes bigint NOT NULL,
  duration_ms bigint NOT NULL,
  missing_since timestamptz,
  split_at timestamptz
);
CREATE INDEX versions_item ON versions (item_id);
CREATE UNIQUE INDEX versions_library_fingerprint_item ON versions (library_id, fingerprint,
  item_id);

CREATE TABLE parts (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  version_id uuid NOT NULL REFERENCES versions(id) ON DELETE CASCADE,
  idx smallint NOT NULL,
  size_bytes bigint NOT NULL,
  duration_ms bigint NOT NULL,
  offset_ms bigint NOT NULL,
  fingerprinted_at timestamptz,
  CONSTRAINT parts_version_id_idx_key UNIQUE (version_id, idx)
);

CREATE TABLE chapters (
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  idx int NOT NULL,
  start_ms bigint NOT NULL,
  end_ms bigint NOT NULL,
  title text,
  PRIMARY KEY (part_id, idx)
);

CREATE TABLE collections (
  item_id uuid PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  origin text NOT NULL
    CONSTRAINT collection_origin CHECK (origin IN ('tmdb', 'user', 'smart', 'list')),
  placement text NOT NULL DEFAULT 'library'
    CONSTRAINT collection_placement CHECK (placement IN ('library', 'home')),
  rule jsonb,
  list_source text,
  list_id text,
  list_missing int NOT NULL DEFAULT 0,
  CONSTRAINT collection_list
    CHECK ((origin = 'list') = ((list_source IS NOT NULL) AND (list_id IS NOT NULL))),
  CONSTRAINT collection_rule CHECK ((origin = 'smart') = (rule IS NOT NULL))
);

CREATE TABLE collection_members (
  collection_id uuid NOT NULL REFERENCES collections(item_id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  position int NOT NULL DEFAULT 0,
  PRIMARY KEY (collection_id, item_id)
);
CREATE INDEX collection_members_item ON collection_members (item_id);

CREATE TABLE conversions (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  max_bitrate_kbps int NOT NULL CHECK (max_bitrate_kbps > 0),
  max_width int NOT NULL CHECK (max_width >= 0),
  state text NOT NULL DEFAULT 'queued'
    CONSTRAINT download_state CHECK (state IN ('queued', 'converting', 'ready', 'failed')),
  progress real NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 1),
  size_bytes bigint,
  error text,
  node_id uuid,
  finished_at timestamptz,
  video_codec text NOT NULL DEFAULT 'h264'
    CONSTRAINT video_codec CHECK (video_codec IN ('h264', 'hevc')),
  video_range text NOT NULL DEFAULT 'sdr'
    CONSTRAINT conversion_video_range CHECK (video_range IN ('sdr', 'hlg', 'hdr10', 'hdr10plus',
      'dv')),
  CONSTRAINT conversions_part_id_max_bitrate_kbps_max_width_video_key UNIQUE (part_id,
    max_bitrate_kbps, max_width, video_codec, video_range)
);

CREATE TABLE people (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  name text NOT NULL,
  photo_url text,
  photo_id uuid UNIQUE,
  biography text,
  born date,
  died date,
  birthplace text,
  described_at timestamptz,
  photo_blurhash text
);
CREATE INDEX people_words ON people USING gin (to_tsvector('simple', search_text(name)));

CREATE TABLE credits (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  source text NOT NULL
    CONSTRAINT credit_source CHECK (source IN ('nfo', 'tmdb',
      'tvdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  kind text NOT NULL
    CONSTRAINT credit_kind CHECK (kind IN ('actor', 'guest_star', 'director', 'writer', 'producer',
      'composer', 'creator')),
  role text NOT NULL DEFAULT '',
  position int NOT NULL,
  PRIMARY KEY (item_id, source, kind, person_id, role)
);
CREATE INDEX credits_person ON credits (person_id);

CREATE TABLE device_sessions (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  token_hash bytea NOT NULL UNIQUE,
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  device_name text NOT NULL,
  client text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz,
  kind text NOT NULL DEFAULT 'device' CONSTRAINT session_kind CHECK (kind IN ('device', 'key')),
  CONSTRAINT key_never_lapses CHECK ((kind = 'key') = (expires_at IS NULL))
);
CREATE INDEX device_sessions_profile ON device_sessions (profile_id);

CREATE TABLE downloads (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  conversion_id uuid REFERENCES conversions(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  session_id uuid NOT NULL REFERENCES device_sessions(id) ON DELETE CASCADE,
  CONSTRAINT downloads_profile_id_session_id_part_id_conversion_id_key
    UNIQUE NULLS NOT DISTINCT (profile_id, session_id, part_id, conversion_id)
);
CREATE INDEX downloads_conversion ON downloads (conversion_id);
CREATE INDEX downloads_item ON downloads (item_id);
CREATE INDEX downloads_part ON downloads (part_id);
CREATE INDEX downloads_session ON downloads (session_id);

CREATE TABLE external_ids (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  provider text NOT NULL
    CONSTRAINT id_provider CHECK (provider IN ('tmdb', 'imdb',
      'tvdb') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  value text NOT NULL,
  source text NOT NULL CONSTRAINT id_source CHECK (source IN ('match', 'nfo', 'path', 'user')),
  PRIMARY KEY (item_id, provider)
);
CREATE INDEX external_ids_value ON external_ids (provider, value);

CREATE TABLE favourites (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  added_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (profile_id, item_id)
);
CREATE INDEX favourites_item ON favourites (item_id);

CREATE TABLE folders (
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  path text NOT NULL,
  fingerprint bytea NOT NULL,
  PRIMARY KEY (library_id, path)
);

CREATE TABLE history_imports (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  source text NOT NULL CONSTRAINT import_source CHECK (source IN ('plex', 'jellyfin', 'emby')),
  url text NOT NULL,
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  source_user text NOT NULL DEFAULT '',
  token text,
  status text NOT NULL DEFAULT 'queued'
    CONSTRAINT import_status CHECK (status IN ('queued', 'running', 'done', 'failed')),
  error text NOT NULL DEFAULT '',
  matched int NOT NULL DEFAULT 0,
  imported int NOT NULL DEFAULT 0,
  skipped int NOT NULL DEFAULT 0,
  unmatched int NOT NULL DEFAULT 0,
  misses jsonb NOT NULL DEFAULT '[]',
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX history_imports_profile ON history_imports (profile_id);

CREATE TABLE home_sections (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  home_row text NOT NULL
    CONSTRAINT home_row CHECK (home_row IN ('continue_watching', 'next_up', 'watchlist',
      'favourites', 'recently_added_films', 'recently_added_shows', 'recently_released',
      'top_rated_unwatched', 'collection')),
  position smallint NOT NULL,
  visibility text NOT NULL CONSTRAINT row_visibility CHECK (visibility IN ('shown', 'hidden')),
  PRIMARY KEY (profile_id, home_row)
);

CREATE TABLE item_fields (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  field text NOT NULL
    CONSTRAINT field CHECK (field IN ('title', 'sort_title', 'original_title', 'overview',
      'tagline', 'certificate', 'release_date', 'year', 'genres', 'studios')),
  source text NOT NULL
    CONSTRAINT field_source CHECK (source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user', 'mdblist',
      'omdb', 'opensubtitles') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (item_id, field)
);

CREATE TABLE jobs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  kind text NOT NULL
    CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'keyframe_walk', 'identify', 'scan_library',
      'markers', 'previews', 'convert', 'deliver_webhook', 'theme', 'probe', 'import_history')),
  subject uuid NOT NULL,
  state text NOT NULL DEFAULT 'queued'
    CONSTRAINT job_state CHECK (state IN ('queued', 'running', 'rerun', 'dead')),
  priority smallint NOT NULL DEFAULT 0,
  attempts smallint NOT NULL DEFAULT 0,
  run_after timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz,
  node_id uuid,
  last_error text,
  due text NOT NULL DEFAULT 'now' CONSTRAINT job_due CHECK (due IN ('now', 'window')),
  CONSTRAINT jobs_kind_subject_key UNIQUE (kind, subject)
);
CREATE INDEX jobs_claim ON jobs (priority DESC, id) WHERE state = 'queued';
CREATE INDEX jobs_dead ON jobs (id) WHERE state = 'dead';
CREATE INDEX jobs_lease ON jobs (lease_until) WHERE state IN ('running', 'rerun');

CREATE TABLE keyframes (
  part_id uuid PRIMARY KEY REFERENCES parts(id) ON DELETE CASCADE,
  pts_ms bigint[] NOT NULL
);

CREATE TABLE leader (
  name text PRIMARY KEY,
  node_id uuid NOT NULL,
  expires_at timestamptz NOT NULL
);

CREATE TABLE library_order (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  position smallint NOT NULL,
  PRIMARY KEY (profile_id, library_id)
);
CREATE INDEX library_order_library ON library_order (library_id);

CREATE TABLE library_remote_extras (
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  kind text NOT NULL
    CONSTRAINT library_extra_kind CHECK (kind IN ('trailer', 'teaser', 'featurette',
      'behind_the_scenes', 'deleted_scene', 'interview', 'scene', 'short', 'clip', 'blooper',
      'theme_video', 'other')),
  PRIMARY KEY (library_id, kind)
);

CREATE TABLE library_sources (
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  source text NOT NULL
    CONSTRAINT library_source CHECK (source IN ('nfo', 'tmdb', 'tvdb', 'mdblist',
      'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  position smallint NOT NULL,
  item_kind text NOT NULL
    CONSTRAINT library_source_item_kind CHECK (item_kind IN ('movie', 'show', 'season', 'episode')),
  fetcher text NOT NULL CONSTRAINT library_source_fetcher CHECK (fetcher IN ('metadata', 'images')),
  enabled boolean NOT NULL,
  PRIMARY KEY (library_id, item_kind, fetcher, source),
  CONSTRAINT library_sources_library_id_item_kind_fetcher_position_key UNIQUE (library_id,
    item_kind, fetcher, "position")
);

CREATE TABLE markers (
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  kind text NOT NULL
    CONSTRAINT marker_kind CHECK (kind IN ('intro', 'credits', 'recap', 'preview')),
  source text NOT NULL
    CONSTRAINT marker_source CHECK (source IN ('user', 'chapter', 'fingerprint', 'blackframes')),
  start_ms bigint CHECK (start_ms >= 0),
  end_ms bigint,
  PRIMARY KEY (part_id, kind, source),
  CONSTRAINT marker_stretch
    CHECK ((start_ms IS NULL) = (end_ms IS NULL) AND (start_ms IS NOT NULL OR source = 'user')),
  CONSTRAINT markers_check CHECK (end_ms > start_ms)
);

CREATE TABLE node (
  id uuid PRIMARY KEY,
  name text NOT NULL,
  first_seen timestamptz NOT NULL DEFAULT now(),
  role text NOT NULL DEFAULT 'all' CONSTRAINT role CHECK (role IN ('all', 'serve', 'transcode')),
  transcode_limit int CONSTRAINT transcode_limit CHECK (transcode_limit >= 0),
  availability text NOT NULL DEFAULT 'active'
    CONSTRAINT availability CHECK (availability IN ('active', 'draining')),
  note text NOT NULL DEFAULT '',
  last_seen timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE part_files (
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  rel_path text NOT NULL,
  size_bytes bigint NOT NULL,
  mtime_ns bigint NOT NULL,
  PRIMARY KEY (library_id, rel_path)
);
CREATE INDEX part_files_part ON part_files (part_id);

CREATE TABLE person_ids (
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  provider text NOT NULL
    CONSTRAINT person_id_provider CHECK (provider IN ('tmdb', 'imdb',
      'tvdb') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  value text NOT NULL,
  PRIMARY KEY (provider, value),
  CONSTRAINT person_ids_person_id_provider_key UNIQUE (person_id, provider)
);

CREATE TABLE playlists (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  name text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX playlists_profile ON playlists (profile_id, name);

CREATE TABLE playlist_entries (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  playlist_id uuid NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  position int NOT NULL
);
CREATE INDEX playlist_entries_item ON playlist_entries (item_id);
CREATE INDEX playlist_entries_order ON playlist_entries (playlist_id, "position");

CREATE TABLE plays (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  version_id uuid REFERENCES versions(id) ON DELETE SET NULL,
  method text NOT NULL CONSTRAINT play_method CHECK (method IN ('direct', 'remux', 'transcode')),
  started_at timestamptz NOT NULL,
  stopped_at timestamptz NOT NULL,
  position_ms bigint NOT NULL
);
CREATE INDEX plays_item ON plays (item_id);
CREATE INDEX plays_profile ON plays (profile_id, stopped_at DESC);
CREATE INDEX plays_stopped ON plays (stopped_at DESC);
CREATE INDEX plays_version ON plays (version_id);

CREATE TABLE plugins (
  slug text PRIMARY KEY CHECK (('plugin:' || slug) ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  url text NOT NULL,
  manifest jsonb NOT NULL
);

CREATE TABLE previews (
  part_id uuid PRIMARY KEY REFERENCES parts(id) ON DELETE CASCADE,
  chapter_images int[] NOT NULL
);

CREATE TABLE profile_libraries (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  PRIMARY KEY (profile_id, library_id)
);
CREATE INDEX profile_libraries_library ON profile_libraries (library_id);

CREATE TABLE profile_preferences (
  profile_id uuid PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,
  audio_language text NOT NULL DEFAULT '',
  audio_track text NOT NULL CONSTRAINT audio_track CHECK (audio_track IN ('default', 'language')),
  subtitle_language text NOT NULL DEFAULT '',
  subtitle_mode text NOT NULL
    CONSTRAINT subtitle_mode CHECK (subtitle_mode IN ('default', 'always', 'only_forced', 'none',
      'smart')),
  remember_audio text NOT NULL
    CONSTRAINT remember_audio CHECK (remember_audio IN ('remember', 'forget')),
  remember_subtitles text NOT NULL
    CONSTRAINT remember_subtitles CHECK (remember_subtitles IN ('remember', 'forget')),
  max_bitrate_kbps int NOT NULL DEFAULT 0 CHECK (max_bitrate_kbps >= 0),
  next_episode text NOT NULL CONSTRAINT next_episode CHECK (next_episode IN ('play', 'offer')),
  intro_action text NOT NULL
    CONSTRAINT intro_action CHECK (intro_action IN ('none', 'ask', 'skip')),
  credits_action text NOT NULL
    CONSTRAINT credits_action CHECK (credits_action IN ('none', 'ask', 'skip')),
  saved_at timestamptz NOT NULL DEFAULT now(),
  theme_music text NOT NULL DEFAULT 'off'
    CONSTRAINT theme_music CHECK (theme_music IN ('play', 'off'))
);

CREATE TABLE providers (
  id text PRIMARY KEY,
  settings jsonb NOT NULL DEFAULT '{}'
);

CREATE TABLE ratings (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL
    CONSTRAINT rating_source CHECK (source IN ('nfo', 'tmdb', 'tvdb', 'mdblist',
      'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  site text NOT NULL
    CONSTRAINT rating_site CHECK (site IN ('imdb', 'tmdb', 'rotten_tomatoes',
      'rotten_tomatoes_audience')),
  score real NOT NULL CHECK (score BETWEEN 0 AND 100),
  votes int,
  PRIMARY KEY (item_id, source, site)
);
CREATE INDEX ratings_site_score ON ratings (site, score DESC, item_id DESC) INCLUDE (votes);

CREATE TABLE remote_videos (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL
    CONSTRAINT remote_video_source CHECK (source IN ('tmdb',
      'tvdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  position smallint NOT NULL,
  kind text NOT NULL
    CONSTRAINT remote_video_kind CHECK (kind IN ('trailer', 'teaser', 'featurette',
      'behind_the_scenes', 'deleted_scene', 'interview', 'scene', 'short', 'clip', 'blooper',
      'theme_video', 'other')),
  site text NOT NULL,
  key text NOT NULL,
  name text NOT NULL,
  language text,
  published_at timestamptz,
  thumb_id uuid,
  PRIMARY KEY (item_id, source, "position")
);
CREATE UNIQUE INDEX remote_videos_thumb ON remote_videos (thumb_id);

CREATE TABLE scan_requests (
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  path text NOT NULL,
  asked_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (library_id, path)
);

CREATE TABLE server (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  created_at timestamptz NOT NULL DEFAULT now(),
  signing_key bytea NOT NULL
    DEFAULT decode(replace(gen_random_uuid()::text || gen_random_uuid()::text, '-', ''), 'hex'),
  certificate_country text,
  maintenance_start smallint NOT NULL DEFAULT 2 CHECK (maintenance_start BETWEEN 0 AND 23),
  maintenance_end smallint NOT NULL DEFAULT 5 CHECK (maintenance_end BETWEEN 0 AND 23),
  maintenance_zone text NOT NULL DEFAULT 'UTC',
  previews_timing text NOT NULL DEFAULT 'window'
    CONSTRAINT previews_timing CHECK (previews_timing IN ('window', 'window_and_added')),
  markers_timing text NOT NULL DEFAULT 'window_and_added'
    CONSTRAINT markers_timing CHECK (markers_timing IN ('window', 'window_and_added')),
  secure_connections text NOT NULL DEFAULT 'disabled'
    CONSTRAINT secure_connections CHECK (secure_connections IN ('required', 'preferred',
      'disabled')),
  tls_certificate text,
  tls_key text,
  jellyfin text NOT NULL DEFAULT 'off' CONSTRAINT jellyfin CHECK (jellyfin IN ('on', 'off')),
  jellyfin_port int NOT NULL DEFAULT 8096
    CONSTRAINT jellyfin_port CHECK (jellyfin_port BETWEEN 1 AND 65535),
  local_networks cidr[] NOT NULL DEFAULT '{}',
  remote_max_bitrate_kbps int NOT NULL DEFAULT 0
    CONSTRAINT remote_max_bitrate_kbps CHECK (remote_max_bitrate_kbps >= 0),
  storage text NOT NULL DEFAULT 'disk' CONSTRAINT storage CHECK (storage IN ('disk', 'bucket')),
  bucket_endpoint text NOT NULL DEFAULT '',
  bucket_name text NOT NULL DEFAULT '',
  bucket_folder text NOT NULL DEFAULT '',
  bucket_region text NOT NULL DEFAULT '',
  bucket_access_key text NOT NULL DEFAULT '',
  bucket_secret_key text NOT NULL DEFAULT '',
  bucket_delivery text NOT NULL DEFAULT 'proxy'
    CONSTRAINT bucket_delivery CHECK (bucket_delivery IN ('proxy', 'redirect')),
  bucket_public_endpoint text NOT NULL DEFAULT '',
  CONSTRAINT maintenance_hours CHECK (maintenance_start <> maintenance_end),
  CONSTRAINT secure_certificate
    CHECK (secure_connections = 'disabled' OR (tls_certificate IS NOT NULL AND tls_key IS NOT NULL)),
  CONSTRAINT storage_bucket CHECK ((storage = 'disk') OR (bucket_name <> ''))
);
CREATE UNIQUE INDEX server_singleton ON server ((true));

CREATE TABLE storage_move (
  one boolean PRIMARY KEY DEFAULT true CONSTRAINT one_move CHECK (one),
  target jsonb NOT NULL,
  started timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE storage_move_sources (
  source uuid PRIMARY KEY,
  move boolean NOT NULL DEFAULT true REFERENCES storage_move(one) ON DELETE CASCADE,
  copied int NOT NULL DEFAULT 0,
  total int NOT NULL DEFAULT 0,
  done boolean NOT NULL DEFAULT false,
  seen timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX storage_move_sources_move ON storage_move_sources (move);

CREATE TABLE streams (
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  idx int NOT NULL,
  kind text NOT NULL CONSTRAINT stream_kind CHECK (kind IN ('video', 'audio', 'subtitle')),
  codec text NOT NULL,
  profile text,
  language text,
  title text,
  is_default boolean NOT NULL,
  forced boolean NOT NULL,
  hearing_impaired boolean NOT NULL,
  commentary boolean NOT NULL,
  width int,
  height int,
  frame_rate double precision,
  video_range text
    CONSTRAINT stream_video_range CHECK (video_range IN ('sdr', 'hlg', 'hdr10', 'hdr10plus', 'dv')),
  dv_profile smallint,
  dv_level smallint,
  dv_compatibility smallint,
  channels int,
  channel_layout text,
  sample_rate int,
  bitrate_kbps int,
  bit_depth smallint,
  level int,
  interlaced boolean NOT NULL DEFAULT false,
  PRIMARY KEY (part_id, idx)
);

CREATE TABLE subtitle_files (
  version_id uuid NOT NULL REFERENCES versions(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  rel_path text NOT NULL,
  codec text NOT NULL,
  language text,
  title text,
  forced boolean NOT NULL,
  is_default boolean NOT NULL,
  hearing_impaired boolean NOT NULL,
  size_bytes bigint NOT NULL,
  mtime_ns bigint NOT NULL,
  id uuid NOT NULL DEFAULT uuidv7() UNIQUE,
  body bytea,
  PRIMARY KEY (library_id, rel_path)
);
CREATE INDEX subtitle_files_version ON subtitle_files (version_id);

CREATE TABLE subtitle_searches (
  version_id uuid NOT NULL REFERENCES versions(id) ON DELETE CASCADE,
  language text NOT NULL,
  searched_at timestamptz NOT NULL,
  PRIMARY KEY (version_id, language)
);

CREATE TABLE task_state (
  key text PRIMARY KEY
    CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database',
      'refresh_metadata', 'sweep_artwork', 'detect_markers', 'backfill_previews',
      'sweep_downloads', 'prune_activity', 'refresh_collections', 'sync_lists', 'fetch_subtitles')),
  started_at timestamptz NOT NULL,
  finished_at timestamptz,
  result text CONSTRAINT task_result CHECK (result IN ('succeeded', 'failed', 'cancelled')),
  error text,
  requested_at timestamptz
);

CREATE TABLE themes (
  id uuid NOT NULL DEFAULT uuidv7() UNIQUE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL CONSTRAINT theme_source CHECK (source IN ('file', 'themerr')),
  place text NOT NULL,
  position smallint NOT NULL,
  folder text,
  PRIMARY KEY (item_id, source, place),
  CONSTRAINT theme_file_has_folder CHECK ((source = 'file') = (folder IS NOT NULL))
);

CREATE TABLE title_groups (
  item_id uuid PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  group_id uuid NOT NULL
);
CREATE INDEX title_groups_group ON title_groups (group_id);

CREATE TABLE title_keys (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  key text NOT NULL,
  PRIMARY KEY (item_id, key)
);
CREATE INDEX title_keys_key ON title_keys (key);

CREATE TABLE trickplay (
  part_id uuid PRIMARY KEY REFERENCES previews(part_id) ON DELETE CASCADE,
  width int NOT NULL,
  height int NOT NULL,
  interval_ms int NOT NULL,
  columns int NOT NULL,
  rows int NOT NULL,
  thumbnails int NOT NULL
);

CREATE TABLE watch_state (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  position_ms bigint NOT NULL DEFAULT 0 CONSTRAINT position_not_negative CHECK (position_ms >= 0),
  plays int NOT NULL DEFAULT 0,
  watched_at timestamptz,
  last_played_at timestamptz,
  audio_stream smallint,
  subtitle_stream smallint,
  subtitle_file uuid,
  changed_at timestamptz,
  PRIMARY KEY (profile_id, item_id)
);
CREATE INDEX watch_state_item ON watch_state (item_id);
CREATE INDEX watch_state_resume ON watch_state (profile_id,
  last_played_at DESC) WHERE position_ms > 0;

CREATE TABLE watchlist (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  added_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (profile_id, item_id)
);
CREATE INDEX watchlist_item ON watchlist (item_id);

CREATE TABLE webhooks (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  url text NOT NULL,
  secret text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE webhook_deliveries (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  webhook_id uuid NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  kind text NOT NULL
    CONSTRAINT delivery_event CHECK (kind IN ('playback.started', 'playback.paused',
      'playback.resumed', 'playback.stopped', 'auth.signed_in', 'auth.sign_in_refused',
      'profile.added', 'profile.removed', 'library.added', 'library.removed', 'library.scanned',
      'library.titles_added', 'task.failed', 'backup.made', 'webhook.test')),
  body text NOT NULL
);
CREATE INDEX webhook_deliveries_webhook ON webhook_deliveries (webhook_id);

CREATE TABLE webhook_events (
  webhook_id uuid NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  kind text NOT NULL
    CONSTRAINT webhook_event CHECK (kind IN ('playback.started', 'playback.paused',
      'playback.resumed', 'playback.stopped', 'auth.signed_in', 'auth.sign_in_refused',
      'profile.added', 'profile.removed', 'library.added', 'library.removed', 'library.scanned',
      'library.titles_added', 'task.failed', 'backup.made')),
  PRIMARY KEY (webhook_id, kind)
);
CREATE INDEX webhook_events_kind ON webhook_events (kind);

-- What a profile may see, as a predicate the planner writes into each query: who is asking is read
-- once a query, and the rule is an expression over each row. A viewer is the age a profile is
-- limited to, whether it sees unrated titles, the libraries it has (NULL for every one) and the
-- certificates it may not see. No profile at all (the server itself asking) is limited by nothing.
CREATE TYPE viewer AS (max_age smallint, unrated text, libraries uuid[], denied text[]);

-- Each country's certificates and the age each is for, from Jellyfin's rating files
-- (Emby.Server.Implementations/Localization/Ratings); 1000 and over are adults-only and banned.
-- 0-PREFER is Jellyfin's own, tried before any country's.
INSERT INTO certificates (country, rating, age) VALUES
  ('0-PREFER', 'AO', 18),
  ('0-PREFER', 'E', 0),
  ('0-PREFER', 'EC', 0),
  ('0-PREFER', 'M', 18),
  ('0-PREFER', 'P', 1000),
  ('0-PREFER', 'RP', 18),
  ('0-PREFER', 'T', 7),
  ('0-PREFER', 'UR', 18),
  ('0-PREFER', 'X', 1000),
  ('0-PREFER', 'XX', 1000),
  ('0-PREFER', 'XXX', 1000),
  ('0-PREFER', 'XXXX', 1000),
  ('AR', '+13', 13),
  ('AR', '+16', 16),
  ('AR', '+18', 18),
  ('AR', '+18 C', 1001),
  ('AR', 'APTA PARA TODO PÚBLICO', 0),
  ('AR', 'ATP', 0),
  ('AR', 'C', 1001),
  ('AR', 'G', 0),
  ('AR', 'R-13', 13),
  ('AR', 'R-17', 17),
  ('AR', 'SAM 13', 13),
  ('AR', 'SAM 16', 16),
  ('AR', 'SAM 18', 18),
  ('AR', 'SAM 18 C', 1001),
  ('AR', 'SAM 18C', 1001),
  ('AR', 'SAM13', 13),
  ('AR', 'SAM16', 16),
  ('AR', 'SAM18', 18),
  ('AR', 'SAM18C', 1001),
  ('AR', 'SP', 10),
  ('AU', '16+', 16),
  ('AU', '18+', 18),
  ('AU', '7+', 7),
  ('AU', 'EXEMPT', 0),
  ('AU', 'G', 0),
  ('AU', 'M', 15),
  ('AU', 'MA', 15),
  ('AU', 'MA 15+', 15),
  ('AU', 'MA15+', 15),
  ('AU', 'PG', 15),
  ('AU', 'R', 18),
  ('AU', 'R 18+', 18),
  ('AU', 'R18+', 18),
  ('AU', 'RC', 1001),
  ('AU', 'X', 1000),
  ('AU', 'X 18', 1000),
  ('AU', 'X 18+', 1000),
  ('AU', 'X18', 1000),
  ('AU', 'X18+', 1000),
  ('BE', '12', 12),
  ('BE', '14', 14),
  ('BE', '16', 16),
  ('BE', '18', 18),
  ('BE', '6', 6),
  ('BE', '9', 9),
  ('BE', 'AL', 0),
  ('BE', 'KNT', 12),
  ('BE', 'KT', 0),
  ('BE', 'MG6', 6),
  ('BE', 'TOUS', 0),
  ('BG', 'A', 0),
  ('BG', 'B', 0),
  ('BG', 'C', 12),
  ('BG', 'D', 16),
  ('BG', 'X', 18),
  ('BR', '10', 10),
  ('BR', '12', 12),
  ('BR', '14', 14),
  ('BR', '16', 16),
  ('BR', '18', 18),
  ('BR', '9', 9),
  ('BR', 'A10', 10),
  ('BR', 'A12', 12),
  ('BR', 'A14', 14),
  ('BR', 'A16', 16),
  ('BR', 'A18', 18),
  ('BR', 'AL', 0),
  ('BR', 'ER', 10),
  ('BR', 'L', 0),
  ('BR', 'LIVRE', 0),
  ('CA', '14+', 14),
  ('CA', '14A', 14),
  ('CA', '16+', 16),
  ('CA', '18+', 18),
  ('CA', '18A', 18),
  ('CA', 'A', 1000),
  ('CA', 'C', 0),
  ('CA', 'C8', 8),
  ('CA', 'E', 0),
  ('CA', 'G', 0),
  ('CA', 'NC-17', 17),
  ('CA', 'PG', 8),
  ('CA', 'PROHIBITED', 1001),
  ('CA', 'R', 18),
  ('CA', 'TV-14', 14),
  ('CA', 'TV-G', 0),
  ('CA', 'TV-MA', 18),
  ('CA', 'TV-PG', 8),
  ('CA', 'TV-Y', 0),
  ('CA', 'TV-Y7', 7),
  ('CA', 'TV-Y7-FV', 7),
  ('CL', '14', 14),
  ('CL', '18', 18),
  ('CL', '18S', 18),
  ('CL', '18V', 18),
  ('CL', '6', 6),
  ('CL', 'TE', 0),
  ('CL', 'TE+7', 7),
  ('CO', '12', 12),
  ('CO', '15', 15),
  ('CO', '18', 18),
  ('CO', '7', 7),
  ('CO', 'PROHIBITED', 1001),
  ('CO', 'T', 0),
  ('CO', 'X', 1000),
  ('CZ', '12+', 12),
  ('CZ', '15+', 15),
  ('CZ', '18+', 18),
  ('CZ', 'U', 0),
  ('DE', '0', 0),
  ('DE', '12', 12),
  ('DE', '16', 16),
  ('DE', '18', 18),
  ('DE', '6', 6),
  ('DE', 'AB 0', 0),
  ('DE', 'AB 12', 12),
  ('DE', 'AB 16', 16),
  ('DE', 'AB 18', 18),
  ('DE', 'AB 6', 6),
  ('DE', 'EDUCATIONAL', 0),
  ('DE', 'FSK 0', 0),
  ('DE', 'FSK 12', 12),
  ('DE', 'FSK 16', 16),
  ('DE', 'FSK 18', 18),
  ('DE', 'FSK 6', 6),
  ('DE', 'FSK-0', 0),
  ('DE', 'FSK-12', 12),
  ('DE', 'FSK-16', 16),
  ('DE', 'FSK-18', 18),
  ('DE', 'FSK-6', 6),
  ('DE', 'FSK0', 0),
  ('DE', 'FSK12', 12),
  ('DE', 'FSK16', 16),
  ('DE', 'FSK18', 18),
  ('DE', 'FSK6', 6),
  ('DE', 'INFOPROGRAMM', 0),
  ('DK', '11', 11),
  ('DK', '12', 12),
  ('DK', '15', 15),
  ('DK', '16', 16),
  ('DK', '7', 7),
  ('DK', 'A', 0),
  ('DK', 'F', 0),
  ('ES', '0+', 0),
  ('ES', '10', 10),
  ('ES', '12', 12),
  ('ES', '12/FIG', 12),
  ('ES', '13', 13),
  ('ES', '14', 14),
  ('ES', '16', 16),
  ('ES', '16/FIG', 16),
  ('ES', '18', 18),
  ('ES', '18/FIG', 18),
  ('ES', '6+', 6),
  ('ES', '7', 11),
  ('ES', '7/FIG', 11),
  ('ES', '7/I', 11),
  ('ES', '7/I/FIG', 11),
  ('ES', '7I', 11),
  ('ES', '9+', 9),
  ('ES', 'A', 0),
  ('ES', 'A/FIG', 0),
  ('ES', 'A/I', 0),
  ('ES', 'A/I/FIG', 0),
  ('ES', 'AI', 0),
  ('ES', 'APTA', 0),
  ('ES', 'BANNED', 1001),
  ('ES', 'ERI', 0),
  ('ES', 'TP', 0),
  ('ES', 'X', 1000),
  ('FI', '12', 12),
  ('FI', '16', 16),
  ('FI', '18', 18),
  ('FI', '7', 7),
  ('FI', 'K-12', 12),
  ('FI', 'K-16', 16),
  ('FI', 'K-18', 18),
  ('FI', 'K-7', 7),
  ('FI', 'K12', 12),
  ('FI', 'K16', 16),
  ('FI', 'K18', 18),
  ('FI', 'K7', 7),
  ('FI', 'KK', 1001),
  ('FI', 'S', 0),
  ('FI', 'T', 0),
  ('FR', '0+', 0),
  ('FR', '10', 10),
  ('FR', '12', 12),
  ('FR', '14+', 14),
  ('FR', '16', 16),
  ('FR', '18', 18),
  ('FR', '6+', 6),
  ('FR', '9+', 9),
  ('FR', 'INTERDIT AUX MOINS DE 10 ANS', 10),
  ('FR', 'INTERDIT AUX MOINS DE 12 ANS', 12),
  ('FR', 'INTERDIT AUX MOINS DE 16 ANS', 16),
  ('FR', 'INTERDIT AUX MOINS DE 18 ANS', 18),
  ('FR', 'PUBLIC AVERTI', 0),
  ('FR', 'TOUS PUBLICS', 0),
  ('FR', 'TP', 0),
  ('FR', 'U', 0),
  ('FR', 'X', 1000),
  ('FR', '–12', 12),
  ('FR', '–16', 16),
  ('FR', '–18', 18),
  ('GB', '0+', 0),
  ('GB', '12', 12),
  ('GB', '12+', 12),
  ('GB', '12A', 12),
  ('GB', '12PG', 12),
  ('GB', '13+', 13),
  ('GB', '14+', 14),
  ('GB', '15', 15),
  ('GB', '16', 16),
  ('GB', '18', 18),
  ('GB', '6+', 6),
  ('GB', '7+', 7),
  ('GB', '9', 9),
  ('GB', 'ADULT', 1000),
  ('GB', 'ALL', 0),
  ('GB', 'CAUTION', 18),
  ('GB', 'E', 0),
  ('GB', 'G', 0),
  ('GB', 'MATURE', 1000),
  ('GB', 'PG', 8),
  ('GB', 'R18', 1000),
  ('GB', 'TEEN', 13),
  ('GB', 'U', 0),
  ('GR', '12', 12),
  ('GR', '13', 13),
  ('GR', '15', 15),
  ('GR', '16', 16),
  ('GR', '17', 17),
  ('GR', '18', 18),
  ('GR', '18+', 18),
  ('GR', 'K', 0),
  ('GR', 'K-12', 12),
  ('GR', 'K-13', 13),
  ('GR', 'K-15', 15),
  ('GR', 'K-16', 16),
  ('GR', 'K-17', 17),
  ('GR', 'K-18', 18),
  ('GR', 'K12', 12),
  ('GR', 'K13', 13),
  ('GR', 'K15', 15),
  ('GR', 'K16', 16),
  ('GR', 'K17', 17),
  ('GR', 'K18', 18),
  ('HU', '12', 12),
  ('HU', '16', 16),
  ('HU', '18', 18),
  ('HU', '6', 6),
  ('HU', 'KN', 0),
  ('HU', 'X', 18),
  ('ID', '13+', 13),
  ('ID', '17+', 17),
  ('ID', '21+', 21),
  ('ID', 'A', 7),
  ('ID', 'A-BO', 7),
  ('ID', 'A7+', 7),
  ('ID', 'ANAK', 7),
  ('ID', 'D', 18),
  ('ID', 'D18+', 18),
  ('ID', 'DEWASA', 18),
  ('ID', 'P', 2),
  ('ID', 'P-BO', 2),
  ('ID', 'P2+', 2),
  ('ID', 'PRASEKOLAH', 2),
  ('ID', 'R', 13),
  ('ID', 'R-BO', 13),
  ('ID', 'R13+', 13),
  ('ID', 'REMAJA', 13),
  ('ID', 'SEMUA UMUR', 0),
  ('ID', 'SU', 0),
  ('ID', 'SU-BO', 0),
  ('IE', '12', 12),
  ('IE', '12A', 12),
  ('IE', '12PG', 12),
  ('IE', '15', 15),
  ('IE', '15A', 15),
  ('IE', '15PG', 15),
  ('IE', '16', 16),
  ('IE', '18', 18),
  ('IE', 'G', 4),
  ('IE', 'PG', 12),
  ('IN', 'A', 18),
  ('IN', 'S', 1001),
  ('IN', 'U', 0),
  ('IN', 'U/A 13+', 13),
  ('IN', 'U/A 16+', 16),
  ('IN', 'U/A 7+', 7),
  ('IN', 'UA', 12),
  ('IT', '10+', 10),
  ('IT', '12+', 12),
  ('IT', '14+', 14),
  ('IT', '18+', 18),
  ('IT', '6+', 6),
  ('IT', 'PER TUTTI', 0),
  ('IT', 'PT', 0),
  ('IT', 'T', 0),
  ('IT', 'VIETATO AI MINORI DI 10 ANNI', 10),
  ('IT', 'VIETATO AI MINORI DI 12 ANNI', 12),
  ('IT', 'VIETATO AI MINORI DI 14 ANNI', 14),
  ('IT', 'VIETATO AI MINORI DI 18 ANNI', 18),
  ('IT', 'VIETATO AI MINORI DI 6 ANNI', 6),
  ('IT', 'VM 10', 10),
  ('IT', 'VM 12', 12),
  ('IT', 'VM 14', 14),
  ('IT', 'VM 18', 18),
  ('IT', 'VM 6', 6),
  ('IT', 'VM-10', 10),
  ('IT', 'VM-12', 12),
  ('IT', 'VM-14', 14),
  ('IT', 'VM-18', 18),
  ('IT', 'VM-6', 6),
  ('IT', 'VM10', 10),
  ('IT', 'VM12', 12),
  ('IT', 'VM14', 14),
  ('IT', 'VM18', 18),
  ('IT', 'VM6', 6),
  ('JP', '15+', 15),
  ('JP', '15A', 15),
  ('JP', '15PG', 15),
  ('JP', '16+', 16),
  ('JP', '18+', 18),
  ('JP', 'A', 0),
  ('JP', 'B', 12),
  ('JP', 'C', 15),
  ('JP', 'D', 17),
  ('JP', 'G', 0),
  ('JP', 'PG12', 12),
  ('JP', 'R15+', 15),
  ('JP', 'Z', 18),
  ('KR', '12', 12),
  ('KR', '15', 15),
  ('KR', '19', 19),
  ('KR', 'ALL', 0),
  ('KR', 'RESTRICTED SCREENING', 1001),
  ('KZ', 'E16', 16),
  ('KZ', 'E18', 18),
  ('KZ', 'HA', 18),
  ('KZ', 'K', 0),
  ('KZ', 'Б14', 14),
  ('KZ', 'БА', 12),
  ('LT', 'N-13', 13),
  ('LT', 'N-16', 16),
  ('LT', 'N-18', 18),
  ('LT', 'N-7', 7),
  ('LT', 'V', 0),
  ('MX', 'A', 0),
  ('MX', 'AA', 0),
  ('MX', 'B', 12),
  ('MX', 'B-15', 15),
  ('MX', 'C', 18),
  ('MX', 'D', 1000),
  ('NL', '12', 12),
  ('NL', '14', 14),
  ('NL', '16', 16),
  ('NL', '18', 18),
  ('NL', '6', 6),
  ('NL', '9', 9),
  ('NL', 'AL', 0),
  ('NL', 'MG6', 6),
  ('NO', '11', 11),
  ('NO', '11 ÅR', 11),
  ('NO', '12', 12),
  ('NO', '12 ÅR', 12),
  ('NO', '15', 15),
  ('NO', '15 ÅR', 15),
  ('NO', '18', 18),
  ('NO', '18 ÅR', 18),
  ('NO', '6', 6),
  ('NO', '6 ÅR', 6),
  ('NO', '7', 7),
  ('NO', '7 ÅR', 7),
  ('NO', '9', 9),
  ('NO', '9 ÅR', 9),
  ('NO', 'A', 0),
  ('NO', 'NOT APPROVED', 1001),
  ('NZ', 'EXEMPT', 0),
  ('NZ', 'G', 0),
  ('NZ', 'GA', 18),
  ('NZ', 'GY', 13),
  ('NZ', 'M', 16),
  ('NZ', 'MA', 1000),
  ('NZ', 'OBJECTIONABLE', 1001),
  ('NZ', 'PG', 13),
  ('NZ', 'R', 1001),
  ('NZ', 'R13', 13),
  ('NZ', 'R15', 15),
  ('NZ', 'R16', 16),
  ('NZ', 'R18', 18),
  ('NZ', 'RP13', 13),
  ('NZ', 'RP16', 16),
  ('NZ', 'RP18', 18),
  ('PH', 'G', 0),
  ('PH', 'PG', 13),
  ('PH', 'R-13', 13),
  ('PH', 'R-16', 16),
  ('PH', 'R-18', 18),
  ('PH', 'X', 1001),
  ('PL', '12', 12),
  ('PL', '16', 16),
  ('PL', '18', 18),
  ('PL', '7', 7),
  ('PL', 'AL', 0),
  ('PL', 'B.O.', 0),
  ('PL', 'OD 12 LAT', 12),
  ('PL', 'OD 16 LAT', 16),
  ('PL', 'OD 18 LAT', 18),
  ('PL', 'OD 7 LAT', 7),
  ('PL', 'R', 18),
  ('PT', 'M/12', 12),
  ('PT', 'M/14', 14),
  ('PT', 'M/16', 16),
  ('PT', 'M/18', 18),
  ('PT', 'M/3', 3),
  ('PT', 'M/6', 6),
  ('PT', 'P', 1000),
  ('PT', 'PÚBLICOS', 0),
  ('RO', '12', 12),
  ('RO', '15', 15),
  ('RO', '18', 18),
  ('RO', '18+', 1000),
  ('RO', 'AG', 0),
  ('RO', 'AP', 0),
  ('RO', 'AP-12', 12),
  ('RO', 'IC', 1001),
  ('RO', 'IM-18', 18),
  ('RO', 'IM-18-XXX', 1000),
  ('RO', 'N-15', 15),
  ('RU', '0+', 0),
  ('RU', '12+', 12),
  ('RU', '16+', 16),
  ('RU', '18+', 18),
  ('RU', '6+', 6),
  ('RU', 'REFUSED CLASSIFICATION', 1001),
  ('SE', '0+', 0),
  ('SE', '10+', 10),
  ('SE', '11', 11),
  ('SE', '11 ÅR', 11),
  ('SE', '11+', 11),
  ('SE', '14', 14),
  ('SE', '15', 15),
  ('SE', '15 ÅR', 15),
  ('SE', '15+', 15),
  ('SE', '18', 18),
  ('SE', '18+', 18),
  ('SE', '7', 7),
  ('SE', '7 ÅR', 7),
  ('SE', '7+', 7),
  ('SE', '9+', 9),
  ('SE', 'ALLA', 0),
  ('SE', 'BARNFÖRBJUDEN', 18),
  ('SE', 'BARNTILLÅTEN', 0),
  ('SE', 'BFJ', 18),
  ('SE', 'BTL', 0),
  ('SE', 'FRÅN 11 ÅR', 11),
  ('SE', 'FRÅN 15 ÅR', 15),
  ('SE', 'FRÅN 7 ÅR', 7),
  ('SG', 'G', 0),
  ('SG', 'M18', 18),
  ('SG', 'NC16', 16),
  ('SG', 'PG', 7),
  ('SG', 'PG13', 13),
  ('SG', 'R21', 21),
  ('SK', '12', 12),
  ('SK', '15', 15),
  ('SK', '18', 18),
  ('SK', '7', 7),
  ('SK', 'NR', 0),
  ('SK', 'U', 0),
  ('TH', '13', 13),
  ('TH', '15', 15),
  ('TH', '18+', 18),
  ('TH', '20', 20),
  ('TH', 'BANNED', 1001),
  ('TH', 'G', 0),
  ('TH', 'P', 0),
  ('TR', '10+', 10),
  ('TR', '10A', 10),
  ('TR', '13+', 13),
  ('TR', '13A', 13),
  ('TR', '16+', 16),
  ('TR', '18+', 18),
  ('TR', '6+', 6),
  ('TR', '6A', 6),
  ('TR', 'GENEL İZLEYICI KITLESI', 0),
  ('TW', '0+', 0),
  ('TW', '12+', 12),
  ('TW', '15+', 15),
  ('TW', '18+', 18),
  ('TW', '6+', 6),
  ('TW', '保護級', 6),
  ('TW', '普', 0),
  ('TW', '普遍級', 0),
  ('TW', '護', 6),
  ('TW', '輔12級', 12),
  ('TW', '輔15級', 15),
  ('TW', '輔導12歲級', 12),
  ('TW', '輔導15歲級', 15),
  ('TW', '輔導級', 12),
  ('TW', '限', 18),
  ('TW', '限制級', 18),
  ('UA', '0+', 0),
  ('UA', '12+', 12),
  ('UA', '16+', 16),
  ('UA', '18+', 18),
  ('US', 'APPROVED', 0),
  ('US', 'G', 0),
  ('US', 'NC-17', 17),
  ('US', 'PG', 10),
  ('US', 'PG-13', 13),
  ('US', 'R', 17),
  ('US', 'TV-14', 14),
  ('US', 'TV-14-D', 14),
  ('US', 'TV-14-DL', 14),
  ('US', 'TV-14-DLS', 14),
  ('US', 'TV-14-DLSV', 14),
  ('US', 'TV-14-DLV', 14),
  ('US', 'TV-14-DS', 14),
  ('US', 'TV-14-DSV', 14),
  ('US', 'TV-14-DV', 14),
  ('US', 'TV-14-L', 14),
  ('US', 'TV-14-LS', 14),
  ('US', 'TV-14-LSV', 14),
  ('US', 'TV-14-LV', 14),
  ('US', 'TV-14-S', 14),
  ('US', 'TV-14-SV', 14),
  ('US', 'TV-14-V', 14),
  ('US', 'TV-AO', 18),
  ('US', 'TV-G', 0),
  ('US', 'TV-MA', 17),
  ('US', 'TV-MA-L', 17),
  ('US', 'TV-MA-LS', 17),
  ('US', 'TV-MA-LSV', 17),
  ('US', 'TV-MA-LV', 17),
  ('US', 'TV-MA-S', 17),
  ('US', 'TV-MA-SV', 17),
  ('US', 'TV-MA-V', 17),
  ('US', 'TV-PG', 10),
  ('US', 'TV-PG-D', 10),
  ('US', 'TV-PG-DL', 10),
  ('US', 'TV-PG-DLS', 10),
  ('US', 'TV-PG-DLSV', 10),
  ('US', 'TV-PG-DLV', 10),
  ('US', 'TV-PG-DS', 10),
  ('US', 'TV-PG-DSV', 10),
  ('US', 'TV-PG-DV', 10),
  ('US', 'TV-PG-L', 10),
  ('US', 'TV-PG-LS', 10),
  ('US', 'TV-PG-LSV', 10),
  ('US', 'TV-PG-LV', 10),
  ('US', 'TV-PG-S', 10),
  ('US', 'TV-PG-SV', 10),
  ('US', 'TV-PG-V', 10),
  ('US', 'TV-X', 18),
  ('US', 'TV-Y', 0),
  ('US', 'TV-Y7', 7),
  ('US', 'TV-Y7-FV', 7),
  ('ZA', '10-12PG', 10),
  ('ZA', '13', 13),
  ('ZA', '16', 16),
  ('ZA', '18', 18),
  ('ZA', '7-9PG', 7),
  ('ZA', 'A', 0),
  ('ZA', 'PG', 7),
  ('ZA', 'X18', 1001),
  ('ZA', 'XX', 1001);


-- The age one certificate is for, as Jellyfin's GetSingleRatingScore reads it: a plain age (16,
-- 18+, France's -12), else the certificate in the country's system, also written with that
-- country before it (GB:15), else the US's, else any country's; else, written COUNTRY:RATING or
-- COUNTRY-RATING, the rating in that country's. NULL is unrated.
-- +goose StatementBegin
CREATE FUNCTION certificate_score(c text, country text) RETURNS smallint LANGUAGE plpgsql STABLE PARALLEL SAFE AS $_$
DECLARE
  r text := upper(trim(regexp_replace(c, '^\s*rated\s*:?\s*', '', 'i')));
  cc text := upper(country);
  found smallint;
  sep text;
  head text;
  tail text;
BEGIN
  IF r IN ('', 'N/A', 'UNRATED', 'NOT RATED', 'NR') THEN
    RETURN NULL;
  END IF;
  IF r ~ '^-?\d{1,4}\+?$' THEN
    RETURN btrim(r, '+-')::smallint;
  END IF;
  SELECT a.age INTO found FROM certificates a
  WHERE a.rating = r OR (a.country = cc AND regexp_replace(r, '^' || cc || '\s*[:-]\s*', '') = a.rating)
  ORDER BY a.country = cc DESC, a.country = 'US' DESC, a.country
  LIMIT 1;
  IF found IS NOT NULL OR position('/' IN r) > 0 THEN
    RETURN found;
  END IF;
  FOREACH sep IN ARRAY ARRAY[':', '-'] LOOP
    head := trim(split_part(r, sep, 1));
    tail := trim(substr(r, position(sep IN r) + 1));
    IF position(sep IN r) > 0 AND tail <> '' AND EXISTS (SELECT 1 FROM certificates WHERE certificates.country = head) THEN
      SELECT a.age INTO found FROM certificates a WHERE a.country = head AND a.rating = tail;
      RETURN coalesce(found, certificate_score(tail, head));
    END IF;
  END LOOP;
  RETURN NULL;
END $_$;
-- +goose StatementEnd

-- The age a certificate is for: the whole of it, else the first of a list (SE:15 / SE:15+) that
-- reads, as Jellyfin's GetRatingScore.
-- +goose StatementBegin
CREATE FUNCTION certificate_age(c text) RETURNS smallint LANGUAGE plpgsql STABLE PARALLEL SAFE AS $$
DECLARE
  country text := (SELECT certificate_country FROM server);
  found smallint := certificate_score(c, country);
  entry text;
BEGIN
  IF found IS NOT NULL OR upper(trim(c)) = 'N/A' THEN
    RETURN found;
  END IF;
  FOREACH entry IN ARRAY string_to_array(c, '/') LOOP
    found := certificate_score(entry, country);
    IF found IS NOT NULL THEN
      RETURN found;
    END IF;
  END LOOP;
  RETURN NULL;
END $$;
-- +goose StatementEnd

-- Its lists are joined rather than selected, so a viewer is a row of plain columns and sees() is
-- written into each query in place of being called for every title.
-- +goose StatementBegin
CREATE FUNCTION viewer(profile uuid) RETURNS SETOF viewer LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
  SELECT p.max_age, p.unrated, l.libraries, d.denied
  FROM (VALUES (profile)) asking (id) LEFT JOIN profiles p ON p.id = asking.id,
    (SELECT array_agg(library_id) FROM profile_libraries WHERE profile_id = profile) l (libraries),
    (WITH RECURSIVE limited AS (SELECT max_age, unrated FROM profiles WHERE id = profile AND max_age IS NOT NULL),
      -- Each certificate once, skipping along the index from one to the next.
      certificate (c) AS (
        SELECT min(certificate) FROM items WHERE EXISTS (SELECT FROM limited)
        UNION ALL
        SELECT (SELECT min(i.certificate) FROM items i WHERE i.certificate > certificate.c) FROM certificate WHERE certificate.c IS NOT NULL
      )
      SELECT coalesce(array_agg(certificate.c), '{}') FROM certificate, limited
      WHERE certificate.c IS NOT NULL AND NOT coalesce(certificate_age(certificate.c) <= limited.max_age, limited.unrated = 'allow')) d (denied)
$$;
-- +goose StatementEnd

-- Whether every certificate from a title up to the film or show it belongs to is within a
-- viewer's age, so an episode's own is honoured beside its show's; with none on the way up it is
-- unrated.
CREATE FUNCTION rated(v viewer,
  item uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN (
  WITH RECURSIVE up AS (
    SELECT i.parent_id, i.certificate FROM items i WHERE i.id = item
    UNION ALL
    SELECT p.parent_id, p.certificate FROM items p JOIN up ON p.id = up.parent_id
  )
  SELECT coalesce(bool_and(up.certificate <> ALL (v.denied)), v.unrated = 'allow')
  FROM up WHERE up.certificate IS NOT NULL
);

-- A title with nothing above it is judged in place, as rated() would judge it, so a wall of films
-- or shows runs no query for each.
CREATE FUNCTION sees(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  (v.libraries IS NULL OR t.library_id = ANY (v.libraries))
  AND (v.max_age IS NULL OR t.kind = 'collection' OR CASE
    WHEN t.parent_id IS NOT NULL THEN rated(v, t.id)
    WHEN t.certificate IS NULL THEN v.unrated = 'allow'
    ELSE t.certificate <> ALL (v.denied)
  END);

-- A title is keyed by every id it is known by, as Jellyfin keeps user data under each of a title's
-- keys: a film's, show's or box set's TMDB, TVDB and IMDb ids, a season's or episode's show's ids
-- with its numbers, and a film's or episode's files. Titles sharing a key are the same, and so is
-- anything sharing one with either: a show TMDB matched in one library is the show TheTVDB matched
-- in another once TMDB has given its TVDB id.
-- +goose StatementBegin
CREATE FUNCTION title_keys_of(item uuid) RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT i.kind || '/' || e.provider || ':' || e.value
  FROM items i JOIN external_ids e ON e.item_id = i.id AND e.provider IN ('tmdb', 'tvdb', 'imdb')
  WHERE i.id = item AND i.kind IN ('movie', 'show', 'collection')
  UNION ALL
  SELECT i.kind || '/' || e.provider || ':' || e.value || '/' || i.season_number
    || CASE i.kind WHEN 'episode' THEN '/' || i.episode_number ELSE '' END
  FROM items i
  JOIN items show ON show.id = CASE i.kind WHEN 'season' THEN i.parent_id ELSE (SELECT s.parent_id FROM items s WHERE s.id = i.parent_id) END
  JOIN external_ids e ON e.item_id = show.id AND e.provider IN ('tmdb', 'tvdb', 'imdb')
  WHERE i.id = item AND i.kind IN ('season', 'episode') AND i.season_number IS NOT NULL
    AND (i.kind = 'season' OR i.episode_number IS NOT NULL)
  UNION ALL
  SELECT i.kind || '/file/' || encode(v.fingerprint, 'hex')
  FROM items i JOIN versions v ON v.item_id = i.id
  WHERE i.id = item AND i.kind IN ('movie', 'episode')
    AND NOT EXISTS (SELECT 1 FROM versions o WHERE o.library_id = v.library_id AND o.fingerprint = v.fingerprint AND o.item_id <> i.id)
$$;
-- +goose StatementEnd

-- Every title the same as one, itself included.
-- +goose StatementBegin
CREATE FUNCTION same_title(item uuid) RETURNS SETOF uuid LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT item
  UNION
  SELECT o.item_id FROM title_groups g JOIN title_groups o ON o.group_id = g.group_id WHERE g.item_id = item
$$;
-- +goose StatementEnd

-- Whether a title is the one of its kind a viewer is shown where every library's titles are
-- gathered: of those the same it may see, the one in the library added first.
CREATE FUNCTION first_of_title(v viewer,
  t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN NOT EXISTS (
  SELECT 1 FROM title_groups g JOIN title_groups o ON o.group_id = g.group_id JOIN items c ON c.id = o.item_id
  WHERE g.item_id = t.id AND (c.library_id, c.id) < (t.library_id, t.id) AND sees(v, c)
);

-- Groups titles again, with those they were the same as before, then makes every profile's state of
-- each the same throughout its group, merged as one row would have kept it.
-- +goose StatementBegin
CREATE FUNCTION group_titles(titles uuid[]) RETURNS void LANGUAGE sql AS $$
  WITH RECURSIVE start AS (
    SELECT unnest(titles) AS id
    UNION
    SELECT o.item_id FROM title_groups g JOIN title_groups o ON o.group_id = g.group_id WHERE g.item_id = ANY (titles)
  ), reach (start, id) AS (
    SELECT start.id, start.id FROM start JOIN items i ON i.id = start.id
    UNION
    SELECT r.start, o.item_id FROM reach r
    JOIN title_keys k ON k.item_id = r.id JOIN title_keys o ON o.key = k.key JOIN items i ON i.id = o.item_id
  ), labelled AS (
    SELECT start, (array_agg(id ORDER BY id))[1] AS group_id FROM reach GROUP BY start
  )
  INSERT INTO title_groups (item_id, group_id)
  SELECT DISTINCT ON (r.id) r.id, l.group_id FROM reach r JOIN labelled l USING (start) ORDER BY r.id
  ON CONFLICT (item_id) DO UPDATE SET group_id = excluded.group_id WHERE title_groups.group_id <> excluded.group_id;

  WITH members AS (
    SELECT o.item_id, o.group_id FROM title_groups o
    WHERE o.group_id IN (SELECT g.group_id FROM title_groups g WHERE g.item_id = ANY (titles))
  ), shared AS (
    SELECT * FROM members WHERE group_id IN (SELECT group_id FROM members GROUP BY group_id HAVING count(*) > 1)
  ), merged AS (
    SELECT w.profile_id, s.group_id,
      (array_agg(w.position_ms ORDER BY w.last_played_at DESC NULLS LAST))[1] AS position_ms,
      max(w.plays) AS plays, min(w.watched_at) AS watched_at, max(w.last_played_at) AS last_played_at
    FROM shared s JOIN watch_state w ON w.item_id = s.item_id
    GROUP BY w.profile_id, s.group_id
  ), states AS (
    INSERT INTO watch_state (profile_id, item_id, position_ms, plays, watched_at, last_played_at)
    SELECT m.profile_id, s.item_id, m.position_ms, m.plays, m.watched_at, m.last_played_at
    FROM merged m JOIN shared s ON s.group_id = m.group_id
    ORDER BY m.profile_id, s.item_id
    ON CONFLICT (profile_id, item_id) DO UPDATE SET position_ms = excluded.position_ms, plays = excluded.plays,
      watched_at = excluded.watched_at, last_played_at = excluded.last_played_at
    WHERE (watch_state.position_ms, watch_state.plays, watch_state.watched_at, watch_state.last_played_at)
      IS DISTINCT FROM (excluded.position_ms, excluded.plays, excluded.watched_at, excluded.last_played_at)
  ), listed AS (
    INSERT INTO watchlist (profile_id, item_id, added_at)
    SELECT l.profile_id, other.item_id, min(l.added_at)
    FROM shared s JOIN watchlist l ON l.item_id = s.item_id JOIN shared other ON other.group_id = s.group_id
    GROUP BY l.profile_id, other.item_id
    ORDER BY l.profile_id, other.item_id
    ON CONFLICT (profile_id, item_id) DO NOTHING
  )
  INSERT INTO favourites (profile_id, item_id, added_at)
  SELECT f.profile_id, other.item_id, min(f.added_at)
  FROM shared s JOIN favourites f ON f.item_id = s.item_id JOIN shared other ON other.group_id = s.group_id
  GROUP BY f.profile_id, other.item_id
  ORDER BY f.profile_id, other.item_id
  ON CONFLICT (profile_id, item_id) DO NOTHING;
$$;
-- +goose StatementEnd

-- Keys titles, their seasons and episodes again and groups them: run where a title is saved or
-- matched, and so may have gained or lost an id.
-- +goose StatementBegin
CREATE FUNCTION key_titles(titles uuid[]) RETURNS void LANGUAGE sql AS $$
  WITH under AS (
    SELECT unnest(titles) AS id
    UNION
    SELECT s.id FROM items s WHERE s.parent_id = ANY (titles)
    UNION
    SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id WHERE s.parent_id = ANY (titles)
  ), keyed AS (
    SELECT under.id, k FROM under, title_keys_of(under.id) k
  ), unkeyed AS (
    DELETE FROM title_keys t USING under
    WHERE t.item_id = under.id AND NOT EXISTS (SELECT 1 FROM keyed WHERE keyed.id = t.item_id AND keyed.k = t.key)
  )
  INSERT INTO title_keys (item_id, key) SELECT id, k FROM keyed ORDER BY id, k ON CONFLICT DO NOTHING;

  SELECT group_titles(ARRAY(
    SELECT unnest(titles)
    UNION
    SELECT s.id FROM items s WHERE s.parent_id = ANY (titles)
    UNION
    SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id WHERE s.parent_id = ANY (titles)
  ));
$$;
-- +goose StatementEnd

-- A title that goes may have been all that joined others: what was its group is grouped again.
-- +goose StatementBegin
CREATE FUNCTION title_left() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM group_titles(ARRAY(SELECT g.item_id FROM title_groups g WHERE g.group_id IN (SELECT group_id FROM gone)));
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER title_left AFTER DELETE ON title_groups REFERENCING OLD TABLE AS gone
  FOR EACH STATEMENT EXECUTE FUNCTION title_left();

INSERT INTO server DEFAULT VALUES;

-- +goose Down
DROP FUNCTION key_titles(uuid[]), group_titles(uuid[]), first_of_title(viewer, items),
  same_title(uuid),
  title_keys_of(uuid), sees(viewer, items), rated(viewer, uuid), viewer(uuid);
DROP TABLE webhook_events, webhook_deliveries, webhooks, watchlist, watch_state, trickplay,
  title_keys, title_groups, themes, task_state, subtitle_searches, subtitle_files, streams,
  storage_move_sources, storage_move, server, scan_requests, remote_videos, ratings, providers,
  profile_preferences, profile_libraries, previews, plugins, plays, playlist_entries, playlists,
  person_ids, part_files, node, markers, library_sources, library_remote_extras, library_order,
  leader, keyframes, jobs, item_fields, home_sections, history_imports, folders, favourites,
  external_ids, downloads, device_sessions, credits, people, conversions, collection_members,
  collections, chapters, parts, versions, certificates, artwork, announced_episodes, activity,
  profiles, items, libraries;
DROP FUNCTION title_left(), certificate_age(text), certificate_score(text, text), search_text(text);
DROP TYPE viewer;
DROP COLLATION title_order;
