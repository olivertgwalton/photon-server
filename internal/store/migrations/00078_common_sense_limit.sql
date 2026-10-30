-- +goose Up
-- A profile may be held to the age Common Sense Media advises a title for, beside its certificate:
-- a title advised for older is hidden. One Common Sense has no age for is judged by its certificate
-- alone, as MDBList carries Common Sense's ages for some titles and not others. A sign-in provider
-- gives the profiles it makes this as it gives their certificate age.
ALTER TABLE profiles ADD COLUMN max_common_sense_age smallint
  CONSTRAINT profile_max_common_sense_age CHECK (max_common_sense_age BETWEEN 1 AND 18);
ALTER TABLE sign_in_providers ADD COLUMN max_common_sense_age smallint
  CONSTRAINT sign_in_provider_max_common_sense_age CHECK (max_common_sense_age BETWEEN 1 AND 18);

ALTER TYPE viewer ADD ATTRIBUTE max_common_sense_age smallint;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION viewer(profile uuid) RETURNS SETOF viewer LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
  SELECT p.max_age, p.unrated, l.libraries, d.denied, p.max_common_sense_age
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
-- +goose StatementEnd

-- Whether no title from item up to the film or show it belongs to is advised for older than a
-- viewer's Common Sense age, so a season and an episode take their show's. A function of its own,
-- as rated() is, so sees() is still inlined.
CREATE FUNCTION advised(v viewer, item uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN NOT EXISTS (
  WITH RECURSIVE up AS (
    SELECT i.id, i.parent_id FROM items i WHERE i.id = item
    UNION ALL
    SELECT p.id, p.parent_id FROM items p JOIN up ON p.id = up.parent_id
  )
  SELECT FROM up JOIN common_sense_ages c ON c.item_id = up.id WHERE c.age > v.max_common_sense_age
);

CREATE OR REPLACE FUNCTION sees(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  CASE WHEN t.kind = 'collection' THEN sees_collection(v, t.id)
  ELSE (v.libraries IS NULL OR t.library_id = ANY (v.libraries))
    AND (v.max_age IS NULL OR CASE
      WHEN t.parent_id IS NOT NULL THEN rated(v, t.id)
      WHEN t.certificate IS NULL THEN v.unrated = 'allow'
      ELSE t.certificate <> ALL (v.denied)
    END)
    AND (v.max_common_sense_age IS NULL OR advised(v, t.id))
  END;

-- +goose Down
CREATE OR REPLACE FUNCTION sees(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  CASE WHEN t.kind = 'collection' THEN sees_collection(v, t.id)
  ELSE (v.libraries IS NULL OR t.library_id = ANY (v.libraries))
    AND (v.max_age IS NULL OR CASE
      WHEN t.parent_id IS NOT NULL THEN rated(v, t.id)
      WHEN t.certificate IS NULL THEN v.unrated = 'allow'
      ELSE t.certificate <> ALL (v.denied)
    END)
  END;
DROP FUNCTION advised(viewer, uuid);
ALTER TYPE viewer DROP ATTRIBUTE max_common_sense_age;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION viewer(profile uuid) RETURNS SETOF viewer LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
  SELECT p.max_age, p.unrated, l.libraries, d.denied
  FROM (VALUES (profile)) asking (id) LEFT JOIN profiles p ON p.id = asking.id,
    (SELECT array_agg(library_id) FROM profile_libraries WHERE profile_id = profile) l (libraries),
    (WITH RECURSIVE limited AS (SELECT max_age, unrated FROM profiles WHERE id = profile AND max_age IS NOT NULL),
      certificate (c) AS (
        SELECT min(certificate) FROM items WHERE EXISTS (SELECT FROM limited)
        UNION ALL
        SELECT (SELECT min(i.certificate) FROM items i WHERE i.certificate > certificate.c) FROM certificate WHERE certificate.c IS NOT NULL
      )
      SELECT coalesce(array_agg(certificate.c), '{}') FROM certificate, limited
      WHERE certificate.c IS NOT NULL AND NOT coalesce(certificate_age(certificate.c) <= limited.max_age, limited.unrated = 'allow')) d (denied)
$$;
-- +goose StatementEnd
ALTER TABLE sign_in_providers DROP COLUMN max_common_sense_age;
ALTER TABLE profiles DROP COLUMN max_common_sense_age;
