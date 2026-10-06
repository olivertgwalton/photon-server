-- +goose Up
-- How each profile plays, on every device, as Jellyfin's user configuration. A profile with no row
-- has never changed them, and plays by the defaults.
CREATE TABLE profile_preferences (
  profile_id uuid PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,
  audio_language text NOT NULL DEFAULT '',
  audio_track text NOT NULL CONSTRAINT audio_track CHECK (audio_track IN ('default', 'language')),
  subtitle_language text NOT NULL DEFAULT '',
  subtitle_mode text NOT NULL
    CONSTRAINT subtitle_mode CHECK (subtitle_mode IN ('default', 'always', 'only_forced', 'none', 'smart')),
  remember_audio text NOT NULL CONSTRAINT remember_audio CHECK (remember_audio IN ('remember', 'forget')),
  remember_subtitles text NOT NULL
    CONSTRAINT remember_subtitles CHECK (remember_subtitles IN ('remember', 'forget')),
  max_bitrate_kbps integer NOT NULL DEFAULT 0 CHECK (max_bitrate_kbps >= 0),
  next_episode text NOT NULL CONSTRAINT next_episode CHECK (next_episode IN ('play', 'offer')),
  intro_action text NOT NULL CONSTRAINT intro_action CHECK (intro_action IN ('none', 'ask', 'skip')),
  credits_action text NOT NULL CONSTRAINT credits_action CHECK (credits_action IN ('none', 'ask', 'skip')),
  saved_at timestamptz NOT NULL DEFAULT now()
);

-- The tracks last chosen for a title, as Jellyfin keeps them in its user data: a stream of the
-- copy, -1 for subtitles off, or a subtitle file. Each is checked against the copy when it is used.
ALTER TABLE watch_state ADD COLUMN audio_stream smallint, ADD COLUMN subtitle_stream smallint,
  ADD COLUMN subtitle_file uuid;

-- +goose Down
ALTER TABLE watch_state DROP COLUMN audio_stream, DROP COLUMN subtitle_stream, DROP COLUMN subtitle_file;
DROP TABLE profile_preferences;
