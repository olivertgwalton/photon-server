-- +goose Up
-- A chapter named with nothing but its time has no name, as the probe now reads one.
UPDATE chapters SET title = NULL WHERE title ~ '^(\(\d+\))?\d{1,2}:\d{2}(:\d{2})?([.,:]\d+)?$';

-- +goose Down
-- Nothing is lost: each time is where its chapter starts.
SELECT 1;
