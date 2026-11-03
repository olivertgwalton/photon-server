-- +goose NO TRANSACTION
-- +goose Up
-- A track's ReplayGain, as its tags give it. A column with no default rewrites nothing, and each
-- statement is its own transaction, so versions is held only for the moment each takes. A music
-- library's folders are forgotten, so its next scan reads its tracks' tags again; unchanged files
-- are not probed again.
ALTER TABLE versions ADD COLUMN track_gain_db double precision, ADD COLUMN track_peak double precision,
  ADD COLUMN album_gain_db double precision, ADD COLUMN album_peak double precision;
DELETE FROM folders WHERE library_id IN (SELECT id FROM libraries WHERE kind = 'music');

-- +goose Down
ALTER TABLE versions DROP COLUMN track_gain_db, DROP COLUMN track_peak, DROP COLUMN album_gain_db,
  DROP COLUMN album_peak;
