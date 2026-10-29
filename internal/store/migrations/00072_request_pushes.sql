-- +goose NO TRANSACTION
-- +goose Up
-- A request's notification is pushed as it is asked, once, as a title's films and episodes are;
-- pushed says it has been. A title's is pushed by its films and episodes, and is always pushed here.
ALTER TABLE notifications ADD COLUMN pushed boolean NOT NULL DEFAULT true;
CREATE INDEX CONCURRENTLY IF NOT EXISTS notifications_unpushed ON notifications (profile_id) WHERE NOT pushed;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS notifications_unpushed;
ALTER TABLE notifications DROP COLUMN pushed;
