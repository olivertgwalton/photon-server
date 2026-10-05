-- +goose Up
-- A subtitle file's id, by which a client fetches it; a file renamed onto another copy keeps it.
ALTER TABLE subtitle_files ADD COLUMN id uuid NOT NULL UNIQUE DEFAULT uuidv7();

-- +goose Down
ALTER TABLE subtitle_files DROP COLUMN id;
