-- +goose Up
-- AniList is a tracker beside Trakt, Simkl and MDBList. Both tables hold a row a tracker or an
-- account, so each is checked again whole.
ALTER TABLE trackers DROP CONSTRAINT tracker,
  ADD CONSTRAINT tracker CHECK (tracker IN ('trakt', 'simkl', 'mdblist', 'anilist'));
ALTER TABLE tracker_accounts DROP CONSTRAINT account_tracker,
  ADD CONSTRAINT account_tracker CHECK (tracker IN ('trakt', 'simkl', 'mdblist', 'anilist'));

-- +goose Down
DELETE FROM tracker_accounts WHERE tracker = 'anilist';
DELETE FROM trackers WHERE tracker = 'anilist';
ALTER TABLE tracker_accounts DROP CONSTRAINT account_tracker,
  ADD CONSTRAINT account_tracker CHECK (tracker IN ('trakt', 'simkl', 'mdblist'));
ALTER TABLE trackers DROP CONSTRAINT tracker,
  ADD CONSTRAINT tracker CHECK (tracker IN ('trakt', 'simkl', 'mdblist'));
