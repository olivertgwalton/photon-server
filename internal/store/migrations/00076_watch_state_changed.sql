-- +goose Up
-- When a profile's watch state of a title last changed, by the time the client says it happened:
-- progress or a mark recorded offline and sent later changes nothing newer. Null until it next
-- changes, and older than anything sent.
ALTER TABLE watch_state ADD COLUMN changed_at timestamptz;

-- +goose Down
ALTER TABLE watch_state DROP COLUMN changed_at;
