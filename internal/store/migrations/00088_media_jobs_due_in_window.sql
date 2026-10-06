-- +goose NO TRANSACTION
-- +goose Up
-- A job's due alone now says whether it waits for the maintenance window. The media jobs 00080
-- left due now that their timing held to the window, and every keyframe walk, wait for it again,
-- rather than all starting at once. A thousand at a time, each its own transaction, so no lock is
-- held long; run again, it finds none left.
-- +goose StatementBegin
DO $$
DECLARE
  n bigint;
BEGIN
  LOOP
    UPDATE jobs SET due = 'window' WHERE id IN (
      SELECT j.id FROM jobs j, server s
      WHERE j.due = 'now' AND j.state IN ('queued', 'rerun') AND (
        j.kind = 'keyframe_walk'
        OR j.kind = 'previews' AND s.previews_timing = 'window'
        OR j.kind = 'markers' AND s.markers_timing = 'window')
      LIMIT 1000);
    GET DIAGNOSTICS n = ROW_COUNT;
    EXIT WHEN n = 0;
    COMMIT;
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Which jobs were due now before is not kept; a job due in the window waits for it either way.
SELECT 1;
