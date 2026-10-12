-- +goose NO TRANSACTION
-- +goose Up
-- A collection is in no library, as Jellyfin's box sets are: it holds films, shows, seasons and
-- episodes of any library, and is listed in its own view and in each library holding its titles.
-- The collections made so far are each in one library; they are dropped, and a provider's box sets
-- are made again, once each, as their titles are next refreshed.
DELETE FROM items WHERE kind = 'collection';

ALTER TABLE items ALTER COLUMN library_id DROP NOT NULL;
ALTER TABLE items ADD CONSTRAINT collection_in_no_library CHECK ((kind = 'collection') = (library_id IS NULL)) NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT collection_in_no_library;

-- A library's wall shows its titles alone: its collections are listed beside it, in its tab and in
-- the Collections view, not among them, so how its wall showed them goes.
ALTER TABLE libraries DROP COLUMN collection_mode;

-- The libraries a smart collection's rule reads, or a list collection's titles are found in.
ALTER TABLE collections ADD COLUMN libraries uuid[]
  CONSTRAINT collection_libraries CHECK ((origin IN ('smart', 'list')) = (libraries IS NOT NULL));

-- A profile sees a collection where it sees one of its titles, as Jellyfin shows a box set, or it
-- holds none yet: an empty one shows nothing but its name. It is a function of its own, taking the
-- collection's id alone, so sees, with no subquery and no whole row, is still inlined into each
-- statement rather than called, and every column read, for every title.
CREATE FUNCTION sees_collection(v viewer, collection uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  NOT EXISTS (SELECT 1 FROM collection_members m WHERE m.collection_id = collection)
  OR EXISTS (SELECT 1 FROM collection_members m JOIN items i ON i.id = m.item_id WHERE m.collection_id = collection AND sees(v, i));

CREATE OR REPLACE FUNCTION sees(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  CASE WHEN t.kind = 'collection' THEN sees_collection(v, t.id)
  ELSE (v.libraries IS NULL OR t.library_id = ANY (v.libraries))
    AND (v.max_age IS NULL OR CASE
      WHEN t.parent_id IS NOT NULL THEN rated(v, t.id)
      WHEN t.certificate IS NULL THEN v.unrated = 'allow'
      ELSE t.certificate <> ALL (v.denied)
    END)
  END;

-- A collection, in no library, ranks its providers' pictures in their own order, after an admin's.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ranked_artwork(titles uuid[]) RETURNS TABLE (id uuid, item_id uuid, source text, kind text,
  place text, "position" smallint, folder text, language text, width int, height int, blurhash text, nth bigint)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT a.id, a.item_id, a.source, a.kind, a.place, a.position, a.folder, a.language, a.width, a.height,
    a.blurhash, row_number() OVER (PARTITION BY a.item_id, a.kind ORDER BY
      CASE a.source WHEN 'user' THEN -2 WHEN 'file' THEN -1 ELSE coalesce(array_position(r.sources, a.source), 32767) END,
      a.position, a.id)
  FROM items i
  LEFT JOIN libraries l ON l.id = i.library_id
  -- The library's order is read once for each title, not once for each of its pictures.
  CROSS JOIN LATERAL (SELECT array_agg(ls.source ORDER BY ls.position) AS sources FROM library_sources ls
    WHERE ls.library_id = l.id AND ls.item_kind = ranked_as(l.kind, i.kind) AND ls.fetcher = 'images' AND ls.enabled) r
  JOIN artwork a ON a.item_id = i.id
  WHERE i.id = ANY(titles)
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ranked_artwork(titles uuid[]) RETURNS TABLE (id uuid, item_id uuid, source text, kind text,
  place text, "position" smallint, folder text, language text, width int, height int, blurhash text, nth bigint)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT a.id, a.item_id, a.source, a.kind, a.place, a.position, a.folder, a.language, a.width, a.height,
    a.blurhash, row_number() OVER (PARTITION BY a.item_id, a.kind ORDER BY
      CASE a.source WHEN 'user' THEN -2 WHEN 'file' THEN -1 ELSE coalesce(array_position(r.sources, a.source), 32767) END,
      a.position, a.id)
  FROM items i
  JOIN libraries l ON l.id = i.library_id
  CROSS JOIN LATERAL (SELECT array_agg(ls.source ORDER BY ls.position) AS sources FROM library_sources ls
    WHERE ls.library_id = l.id AND ls.item_kind = ranked_as(l.kind, i.kind) AND ls.fetcher = 'images' AND ls.enabled) r
  JOIN artwork a ON a.item_id = i.id
  WHERE i.id = ANY(titles)
$$;
-- +goose StatementEnd
CREATE OR REPLACE FUNCTION sees(v viewer, t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  (v.libraries IS NULL OR t.library_id = ANY (v.libraries))
  AND (v.max_age IS NULL OR t.kind = 'collection' OR CASE
    WHEN t.parent_id IS NOT NULL THEN rated(v, t.id)
    WHEN t.certificate IS NULL THEN v.unrated = 'allow'
    ELSE t.certificate <> ALL (v.denied)
  END);
DROP FUNCTION sees_collection(viewer, uuid);
DELETE FROM items WHERE kind = 'collection';
ALTER TABLE collections DROP COLUMN libraries;
ALTER TABLE libraries ADD COLUMN collection_mode text NOT NULL DEFAULT 'grouped'
  CONSTRAINT collection_mode CHECK (collection_mode IN ('grouped', 'shown', 'hidden'));
ALTER TABLE items DROP CONSTRAINT collection_in_no_library;
ALTER TABLE items ALTER COLUMN library_id SET NOT NULL;
