-- +goose NO TRANSACTION
-- +goose Up
-- A download asked for at no bitrate is converted only to fit a width, and its conversion keeps
-- the bitrate it has. Its check is checked once more apart from its change, reading the table
-- without stopping its writes.
ALTER TABLE conversions DROP CONSTRAINT conversions_max_bitrate_kbps_check,
  ADD CONSTRAINT conversion_max_bitrate CHECK (max_bitrate_kbps >= 0) NOT VALID;
ALTER TABLE conversions VALIDATE CONSTRAINT conversion_max_bitrate;

-- +goose Down
-- An uncapped conversion has no place in a table that needs a bitrate, and its downloads go with it.
DELETE FROM conversions WHERE max_bitrate_kbps = 0;
ALTER TABLE conversions DROP CONSTRAINT conversion_max_bitrate,
  ADD CONSTRAINT conversions_max_bitrate_kbps_check CHECK (max_bitrate_kbps > 0) NOT VALID;
ALTER TABLE conversions VALIDATE CONSTRAINT conversions_max_bitrate_kbps_check;
