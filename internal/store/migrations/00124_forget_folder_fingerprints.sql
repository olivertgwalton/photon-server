-- +goose Up
-- A folder was remembered even when a file in it could not be read, and nothing says which, so
-- the next scan reads every folder again. A file it has recorded is not read again.
DELETE FROM folders;

-- +goose Down
-- Nothing is lost: the next scan remembers each folder it reads.
SELECT 1;
