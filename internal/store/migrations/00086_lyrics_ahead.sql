-- +goose NO TRANSACTION
-- +goose Up
-- A version says how the lyrics its own file carries are timed, and a source's kept lyrics how
-- theirs are, so a track says it has lyrics before they are asked for. A column with no default
-- rewrites nothing, and each check is checked again apart from its change, reading the table
-- without stopping its writes. Lyrics are fetched by a job of their own, as a scan finds a track. A
-- music library's folders are forgotten, so its next scan reads its tracks' lyrics and asks for
-- what they lack; unchanged files are not probed again.
ALTER TABLE versions ADD COLUMN lyrics text,
  ADD CONSTRAINT version_lyrics CHECK (lyrics IN ('line', 'untimed')) NOT VALID;
ALTER TABLE versions VALIDATE CONSTRAINT version_lyrics;
ALTER TABLE fetched_lyrics ADD COLUMN timing text,
  ADD CONSTRAINT fetched_lyrics_timing CHECK (timing IN ('line', 'untimed')) NOT VALID;
ALTER TABLE fetched_lyrics VALIDATE CONSTRAINT fetched_lyrics_timing;
UPDATE fetched_lyrics SET timing = CASE WHEN lyrics ~ '\[[0-9]+:[0-9]{2}' THEN 'line' ELSE 'untimed' END
WHERE lyrics IS NOT NULL;
ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history', 'segments', 'push', 'send_request', 'lyrics')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;
DELETE FROM folders WHERE library_id IN (SELECT id FROM libraries WHERE kind = 'music');

-- +goose Down
DELETE FROM jobs WHERE kind = 'lyrics';
ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history', 'segments', 'push', 'send_request')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;
ALTER TABLE fetched_lyrics DROP COLUMN timing;
ALTER TABLE versions DROP COLUMN lyrics;
