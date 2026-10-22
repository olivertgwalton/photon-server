-- +goose Up
-- What becomes of a version on no disk is named for the version, as the rest of the schema names
-- it. A rename is a change of the catalogue alone, and libraries is a row per library.
ALTER TABLE libraries RENAME COLUMN missing_copies TO missing_versions;
ALTER TABLE libraries RENAME CONSTRAINT missing_copy TO missing_version;

-- +goose Down
ALTER TABLE libraries RENAME CONSTRAINT missing_version TO missing_copy;
ALTER TABLE libraries RENAME COLUMN missing_versions TO missing_copies;
