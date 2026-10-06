-- +goose Up
-- The maintenance window, in whole hours of the day in its time zone, 02:00 to 05:00 as Plex's
-- is by default; and when work that reads media waits for it: previews as Plex's chapter
-- thumbnails do by default, intros and credits found by sound also as each part is added, as
-- Plex's intro markers are.
ALTER TABLE server
  ADD COLUMN maintenance_start smallint NOT NULL DEFAULT 2 CHECK (maintenance_start BETWEEN 0 AND 23),
  ADD COLUMN maintenance_end smallint NOT NULL DEFAULT 5 CHECK (maintenance_end BETWEEN 0 AND 23),
  ADD COLUMN maintenance_zone text NOT NULL DEFAULT 'UTC',
  ADD COLUMN previews_timing text NOT NULL DEFAULT 'window'
    CONSTRAINT previews_timing CHECK (previews_timing IN ('window', 'window_and_added')),
  ADD COLUMN markers_timing text NOT NULL DEFAULT 'window_and_added'
    CONSTRAINT markers_timing CHECK (markers_timing IN ('window', 'window_and_added')),
  ADD CONSTRAINT maintenance_hours CHECK (maintenance_start <> maintenance_end);

-- +goose Down
ALTER TABLE server
  DROP COLUMN maintenance_start, DROP COLUMN maintenance_end, DROP COLUMN maintenance_zone,
  DROP COLUMN previews_timing, DROP COLUMN markers_timing;
