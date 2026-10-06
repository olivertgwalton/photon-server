-- +goose Up
-- A limited viewer carries the certificates it may not see, each certificate titles carry judged
-- once a query, where every title read its own and its show's through certificate_age(). A film's
-- or show's verdict is then a lookup in that list, and an episode's a walk up its show with no
-- certificate read on the way.
ALTER TYPE viewer ADD ATTRIBUTE denied text[];

-- Its lists are joined rather than selected, so a viewer is a row of plain columns and sees() is
-- written into each query in place of being called for every title.
CREATE OR REPLACE FUNCTION viewer(profile uuid) RETURNS SETOF viewer LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
  SELECT p.max_age, p.unrated, l.libraries, d.denied
  FROM (VALUES (profile)) asking (id) LEFT JOIN profiles p ON p.id = asking.id,
    (SELECT array_agg(library_id) FROM profile_libraries WHERE profile_id = profile) l (libraries),
    (WITH RECURSIVE limited AS (SELECT max_age, unrated FROM profiles WHERE id = profile AND max_age IS NOT NULL),
      -- Each certificate once, skipping along the index from one to the next.
      certificate (c) AS (
        SELECT min(certificate) FROM items WHERE EXISTS (SELECT FROM limited)
        UNION ALL
        SELECT (SELECT min(i.certificate) FROM items i WHERE i.certificate > certificate.c) FROM certificate WHERE certificate.c IS NOT NULL
      )
      SELECT coalesce(array_agg(certificate.c), '{}') FROM certificate, limited
      WHERE certificate.c IS NOT NULL AND NOT coalesce(certificate_age(certificate.c) <= limited.max_age, limited.unrated = 'allow')) d (denied)
$$;

CREATE OR REPLACE FUNCTION rated(v viewer, item uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN (
  WITH RECURSIVE up AS (
    SELECT i.parent_id, i.certificate FROM items i WHERE i.id = item
    UNION ALL
    SELECT p.parent_id, p.certificate FROM items p JOIN up ON p.id = up.parent_id
  )
  SELECT coalesce(bool_and(up.certificate <> ALL (v.denied)), v.unrated = 'allow')
  FROM up WHERE up.certificate IS NOT NULL
);

-- A title with nothing above it is judged in place, as rated() would judge it, so a wall of films
-- or shows runs no query for each.
CREATE OR REPLACE FUNCTION sees(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  (v.libraries IS NULL OR t.library_id = ANY (v.libraries))
  AND (v.max_age IS NULL OR t.kind = 'collection' OR CASE
    WHEN t.parent_id IS NOT NULL THEN rated(v, t.id)
    WHEN t.certificate IS NULL THEN v.unrated = 'allow'
    ELSE t.certificate <> ALL (v.denied)
  END);

-- +goose Down
CREATE OR REPLACE FUNCTION sees(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  (v.libraries IS NULL OR t.library_id = ANY (v.libraries))
  AND (v.max_age IS NULL OR t.kind = 'collection' OR rated(v, t.id));

CREATE OR REPLACE FUNCTION rated(v viewer, item uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN (
  WITH RECURSIVE up AS (
    SELECT i.parent_id, i.certificate FROM items i WHERE i.id = item
    UNION ALL
    SELECT p.parent_id, p.certificate FROM items p JOIN up ON p.id = up.parent_id
  )
  SELECT coalesce(bool_and(coalesce(certificate_age(up.certificate) <= v.max_age, v.unrated = 'allow')), v.unrated = 'allow')
  FROM up WHERE up.certificate IS NOT NULL
);

ALTER TYPE viewer DROP ATTRIBUTE denied;

CREATE OR REPLACE FUNCTION viewer(profile uuid) RETURNS SETOF viewer LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
  SELECT p.max_age, p.unrated, (SELECT array_agg(l.library_id) FROM profile_libraries l WHERE l.profile_id = profile)
  FROM (VALUES (profile)) asking (id) LEFT JOIN profiles p ON p.id = asking.id
$$;
