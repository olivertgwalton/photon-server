-- +goose NO TRANSACTION
-- +goose Up
-- A film's credits are found in its picture. The check is validated apart, reading the table
-- without stopping its writes.
ALTER TABLE markers DROP CONSTRAINT IF EXISTS marker_source,
  ADD CONSTRAINT marker_source CHECK (source IN ('user', 'chapter', 'fingerprint', 'blackframes')) NOT VALID;
ALTER TABLE markers VALIDATE CONSTRAINT marker_source;

-- +goose Down
DELETE FROM markers WHERE source = 'blackframes';
ALTER TABLE markers DROP CONSTRAINT IF EXISTS marker_source,
  ADD CONSTRAINT marker_source CHECK (source IN ('user', 'chapter', 'fingerprint')) NOT VALID;
ALTER TABLE markers VALIDATE CONSTRAINT marker_source;
