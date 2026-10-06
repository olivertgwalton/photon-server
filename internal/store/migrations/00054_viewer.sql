-- +goose Up
-- What a profile may see, as a predicate the planner writes into each query: who is asking is read
-- once a query, and the rule is an expression over each row, where visible() ran a recursive
-- query for every row of every wall.

-- The age a profile is limited to, whether it sees unrated titles, and the libraries it has, NULL
-- for every one. No profile at all (the server itself asking) is limited by nothing.
CREATE TYPE viewer AS (max_age smallint, unrated text, libraries uuid[]);

CREATE FUNCTION viewer(profile uuid) RETURNS SETOF viewer LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
  SELECT p.max_age, p.unrated, (SELECT array_agg(l.library_id) FROM profile_libraries l WHERE l.profile_id = profile)
  FROM (VALUES (profile)) asking (id) LEFT JOIN profiles p ON p.id = asking.id
$$;

-- Whether every certificate from a title up to the film or show it belongs to is within a
-- viewer's age, so an episode's own is honoured beside its show's; with none on the way up it is
-- unrated.
CREATE FUNCTION rated(v viewer, item uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN (
  WITH RECURSIVE up AS (
    SELECT i.parent_id, i.certificate FROM items i WHERE i.id = item
    UNION ALL
    SELECT p.parent_id, p.certificate FROM items p JOIN up ON p.id = up.parent_id
  )
  SELECT coalesce(bool_and(coalesce(certificate_age(up.certificate) <= v.max_age, v.unrated = 'allow')), v.unrated = 'allow')
  FROM up WHERE up.certificate IS NOT NULL
);

-- Whether a viewer may see a title: it is in a library the viewer has, and rated within its age.
-- A collection is seen as its library. Only a limited viewer reads certificates.
CREATE FUNCTION sees(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  (v.libraries IS NULL OR t.library_id = ANY (v.libraries))
  AND (v.max_age IS NULL OR t.kind = 'collection' OR rated(v, t.id));

DROP FUNCTION visible(uuid, uuid);

-- +goose Down
CREATE FUNCTION visible(item uuid, profile uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN (
  WITH RECURSIVE up AS (
    SELECT i.id, i.parent_id, i.kind, i.certificate, i.library_id FROM items i WHERE i.id = item
    UNION ALL
    SELECT p.id, p.parent_id, p.kind, p.certificate, p.library_id FROM items p JOIN up ON p.id = up.parent_id
  ),
  top AS (SELECT * FROM up WHERE parent_id IS NULL),
  me AS (SELECT * FROM profiles WHERE id = profile)
  SELECT
    (NOT EXISTS (SELECT 1 FROM profile_libraries l WHERE l.profile_id = profile)
      OR EXISTS (SELECT 1 FROM profile_libraries l, top WHERE l.profile_id = profile AND l.library_id = top.library_id))
    AND (me.max_age IS NULL OR top.kind = 'collection'
      OR coalesce(
        (SELECT bool_and(coalesce(certificate_age(up.certificate) <= me.max_age, me.unrated = 'allow')) FROM up WHERE up.certificate IS NOT NULL),
        me.unrated = 'allow'))
  -- No profile at all (the server itself asking) sees everything.
  FROM top LEFT JOIN me ON true
);
DROP FUNCTION sees(viewer, items);
DROP FUNCTION rated(viewer, uuid);
DROP FUNCTION viewer(uuid);
DROP TYPE viewer;
