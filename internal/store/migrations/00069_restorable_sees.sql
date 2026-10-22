-- +goose Up
-- sees and sees_collection call each other. A function with a standard body is read as it is made,
-- so pg_dump could order neither before the other, and no dump of the database restored: each
-- pg_restore stopped at whichever came first. sees_collection's body is now a string, read only as
-- it is called, which pg_dump makes first and sees after it. Both are still inlined.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION sees_collection(v viewer, collection uuid) RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT NOT EXISTS (SELECT 1 FROM collection_members m WHERE m.collection_id = collection)
    OR EXISTS (SELECT 1 FROM collection_members m JOIN items i ON i.id = m.item_id
      WHERE m.collection_id = collection AND sees(v, i))
$$;
-- +goose StatementEnd

-- +goose Down
CREATE OR REPLACE FUNCTION sees_collection(v viewer, collection uuid) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN
  NOT EXISTS (SELECT 1 FROM collection_members m WHERE m.collection_id = collection)
  OR EXISTS (SELECT 1 FROM collection_members m JOIN items i ON i.id = m.item_id WHERE m.collection_id = collection AND sees(v, i));
