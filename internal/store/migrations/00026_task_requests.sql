-- +goose Up
-- When an admin last asked for a task to run now; the scheduler runs it if it has not started since.
ALTER TABLE task_state ADD COLUMN requested_at timestamptz;

-- +goose Down
ALTER TABLE task_state DROP COLUMN requested_at;
