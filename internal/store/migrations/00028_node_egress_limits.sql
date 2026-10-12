-- +goose Up
-- How fast a node sends its players media, in megabits a second, as an admin lets it; zero is no
-- limit.
ALTER TABLE node ADD COLUMN egress_limit_mbps int NOT NULL DEFAULT 0
  CONSTRAINT egress_limit_mbps CHECK (egress_limit_mbps >= 0);

-- +goose Down
ALTER TABLE node DROP COLUMN egress_limit_mbps;
