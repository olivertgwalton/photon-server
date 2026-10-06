-- +goose Up
-- A conversion is to the video a device plays: HEVC or H.264, and the source's HDR kept or tone
-- mapped to SDR. Each is a conversion of its own; those from before were H.264 and SDR.
ALTER TABLE conversions
  ADD COLUMN video_codec text NOT NULL DEFAULT 'h264'
    CONSTRAINT video_codec CHECK (video_codec IN ('h264', 'hevc')),
  ADD COLUMN video_range text NOT NULL DEFAULT 'sdr'
    CONSTRAINT conversion_video_range CHECK (video_range IN ('sdr', 'hlg', 'hdr10', 'hdr10plus', 'dv')),
  DROP CONSTRAINT conversions_part_id_max_bitrate_kbps_max_width_key,
  ADD CONSTRAINT conversions_part_id_max_bitrate_kbps_max_width_video_key
    UNIQUE (part_id, max_bitrate_kbps, max_width, video_codec, video_range);

-- +goose Down
DELETE FROM conversions WHERE video_codec <> 'h264' OR video_range <> 'sdr';
ALTER TABLE conversions DROP CONSTRAINT conversions_part_id_max_bitrate_kbps_max_width_video_key,
  ADD CONSTRAINT conversions_part_id_max_bitrate_kbps_max_width_key UNIQUE (part_id, max_bitrate_kbps, max_width),
  DROP COLUMN video_range,
  DROP COLUMN video_codec;
