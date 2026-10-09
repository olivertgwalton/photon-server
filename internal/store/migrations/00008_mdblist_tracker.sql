-- +goose Up
-- MDBList is a tracker beside Trakt and Simkl. Both tables hold a row a tracker or an account, so
-- each is checked again whole.
ALTER TABLE trackers DROP CONSTRAINT tracker,
  ADD CONSTRAINT tracker CHECK (tracker IN ('trakt', 'simkl', 'mdblist'));
ALTER TABLE tracker_accounts DROP CONSTRAINT account_tracker,
  ADD CONSTRAINT account_tracker CHECK (tracker IN ('trakt', 'simkl', 'mdblist'));

-- +goose Down
DELETE FROM tracker_accounts WHERE tracker = 'mdblist';
DELETE FROM trackers WHERE tracker = 'mdblist';
ALTER TABLE tracker_accounts DROP CONSTRAINT account_tracker,
  ADD CONSTRAINT account_tracker CHECK (tracker IN ('trakt', 'simkl'));
ALTER TABLE trackers DROP CONSTRAINT tracker,
  ADD CONSTRAINT tracker CHECK (tracker IN ('trakt', 'simkl'));
