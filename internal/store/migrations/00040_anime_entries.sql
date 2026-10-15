-- +goose Up
-- The anime lists: where each AniDB entry falls in a show on TheTVDB and TMDB, or the film it is,
-- and its AniList entry. A season of -1 is the show's absolute order. A daily task replaces the
-- rows from the published lists.
CREATE TABLE anime_entries (
  anidb_id integer PRIMARY KEY,
  anilist_id integer,
  tvdb_show integer,
  tvdb_season integer NOT NULL,
  tvdb_offset integer NOT NULL,
  tmdb_show integer,
  tmdb_season integer NOT NULL,
  tmdb_offset integer NOT NULL,
  tmdb_movies integer[] NOT NULL,
  imdb_ids text[] NOT NULL,
  mappings jsonb NOT NULL
);
CREATE INDEX anime_entries_tvdb_show ON anime_entries (tvdb_show);
CREATE INDEX anime_entries_tmdb_show ON anime_entries (tmdb_show);
CREATE INDEX anime_entries_anilist_id ON anime_entries (anilist_id);
ALTER TABLE task_state DROP CONSTRAINT task_key, ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries',
  'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers',
  'backfill_previews', 'sweep_downloads', 'prune_activity', 'refresh_collections', 'sync_lists',
  'fetch_subtitles', 'refresh_anime_lists'));

-- +goose Down
DELETE FROM task_state WHERE key = 'refresh_anime_lists';
ALTER TABLE task_state DROP CONSTRAINT task_key, ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries',
  'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers',
  'backfill_previews', 'sweep_downloads', 'prune_activity', 'refresh_collections', 'sync_lists',
  'fetch_subtitles'));
DROP TABLE anime_entries;
