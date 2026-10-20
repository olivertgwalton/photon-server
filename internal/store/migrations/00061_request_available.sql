-- +goose Up
-- A request is available once a title with the id it was found under is identified in a library,
-- and names that title. A title asked for again after it has gone is a new request.
ALTER TABLE requests ADD COLUMN item_id uuid REFERENCES items(id) ON DELETE SET NULL,
  ADD COLUMN available_at timestamptz,
  DROP CONSTRAINT request_state,
  ADD CONSTRAINT request_state CHECK (state IN ('pending', 'approved', 'declined', 'sent', 'failed', 'available')),
  -- A title may come while its request still waits, which no one then decides.
  DROP CONSTRAINT request_decided,
  ADD CONSTRAINT request_decided CHECK (CASE state WHEN 'pending' THEN decided_at IS NULL
    WHEN 'available' THEN true ELSE decided_at IS NOT NULL END);
CREATE INDEX requests_item ON requests (item_id);
DROP INDEX requests_open;
CREATE UNIQUE INDEX requests_open ON requests (library_id, provider, value) WHERE state NOT IN ('declined', 'available');

-- +goose Down
DROP INDEX requests_open;
DELETE FROM requests WHERE state = 'available';
CREATE UNIQUE INDEX requests_open ON requests (library_id, provider, value) WHERE state <> 'declined';
DROP INDEX requests_item;
ALTER TABLE requests DROP CONSTRAINT request_decided,
  ADD CONSTRAINT request_decided CHECK ((state = 'pending') = (decided_at IS NULL)),
  DROP CONSTRAINT request_state,
  ADD CONSTRAINT request_state CHECK (state IN ('pending', 'approved', 'declined', 'sent', 'failed')),
  DROP COLUMN available_at, DROP COLUMN item_id;
