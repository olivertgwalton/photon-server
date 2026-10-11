-- +goose Up
-- An episode judged in place with its season and show, as rated() would judge it, so a wall of
-- episodes runs no query for each.
CREATE FUNCTION sees(v viewer, t items, season items, show items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  (v.libraries IS NULL OR t.library_id = ANY (v.libraries))
  AND (v.max_age IS NULL OR CASE
    WHEN t.certificate IS NULL AND season.certificate IS NULL AND show.certificate IS NULL THEN v.unrated = 'allow'
    ELSE coalesce(t.certificate <> ALL (v.denied), true) AND coalesce(season.certificate <> ALL (v.denied), true)
      AND coalesce(show.certificate <> ALL (v.denied), true)
  END);

-- +goose Down
DROP FUNCTION sees(viewer, items, items, items);
