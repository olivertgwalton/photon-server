-- +goose Up
-- What a node encodes video on, as an admin sets it: an acceleration, and the device it is on,
-- none the first; none is the first the node finds that encodes, as it does where nothing is set.
ALTER TABLE node
  ADD COLUMN acceleration text
    CONSTRAINT acceleration CHECK (acceleration IN ('software', 'videotoolbox', 'vaapi', 'qsv', 'nvenc', 'amf')),
  ADD COLUMN encoder_device text NOT NULL DEFAULT ''
    CONSTRAINT encoder_device CHECK (acceleration IS NOT NULL OR encoder_device = '');

-- +goose Down
ALTER TABLE node DROP COLUMN acceleration, DROP COLUMN encoder_device;
