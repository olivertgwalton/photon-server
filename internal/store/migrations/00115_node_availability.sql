-- +goose Up
-- Whether a node takes new work, and why not, for other admins: one drained plays its streams to
-- their end and is given nothing new.
ALTER TABLE node
  ADD COLUMN availability text NOT NULL DEFAULT 'active'
    CONSTRAINT availability CHECK (availability IN ('active', 'draining')),
  ADD COLUMN note text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE node DROP COLUMN availability, DROP COLUMN note;
