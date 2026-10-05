-- +goose Up
-- What a profile may see: titles rated no older than max_age, unrated ones as unrated says, and
-- only the libraries listed for it, every library where none are.
ALTER TABLE profiles ADD COLUMN max_age smallint CHECK (max_age >= 0),
  ADD COLUMN unrated text NOT NULL DEFAULT 'allow' CONSTRAINT profile_unrated CHECK (unrated IN ('allow', 'block'));

CREATE TABLE profile_libraries (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  PRIMARY KEY (profile_id, library_id)
);

-- The youngest age a certificate is for, across the systems providers give: the BBFC's, the
-- MPA's, the US TV guidelines, and the plain ages most countries use (FSK 12, 16+). NULL is a
-- certificate no system here knows: unrated.
CREATE FUNCTION certificate_age(c text) RETURNS smallint LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN
  CASE
    WHEN upper(trim(c)) IN ('G', 'U', 'UC', 'E', 'AL', 'TP', 'TV-Y', 'TV-G', 'ALL') THEN 0
    WHEN upper(trim(c)) IN ('TV-Y7', 'TV-Y7-FV') THEN 7
    WHEN upper(trim(c)) IN ('PG', 'TV-PG') THEN 10
    WHEN upper(trim(c)) IN ('12A', 'PG-12') THEN 12
    WHEN upper(trim(c)) IN ('PG-13') THEN 13
    WHEN upper(trim(c)) IN ('TV-14') THEN 14
    WHEN upper(trim(c)) IN ('M', 'MA15+') THEN 15
    WHEN upper(trim(c)) IN ('R', 'TV-MA') THEN 17
    WHEN upper(trim(c)) IN ('NC-17', 'R18', 'R18+', 'X', 'XXX') THEN 18
    WHEN regexp_replace(c, '\D', '', 'g') ~ '^\d{1,2}$' THEN regexp_replace(c, '\D', '', 'g')::smallint
  END;

-- Whether a profile may see a title: it is in a library the profile has, and it, or the film or
-- show it belongs to, is rated within the profile's age. A collection is seen as its library.
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
      OR coalesce(certificate_age(top.certificate) <= me.max_age, me.unrated = 'allow'))
  -- No profile at all (the server itself asking) sees everything.
  FROM top LEFT JOIN me ON true
);

-- +goose Down
DROP FUNCTION visible(uuid, uuid);
DROP FUNCTION certificate_age(text);
DROP TABLE profile_libraries;
ALTER TABLE profiles DROP COLUMN unrated, DROP COLUMN max_age;
