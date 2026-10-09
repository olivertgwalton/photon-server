-- +goose Up
-- Whether a title is the first of those the same a viewer sees is asked as whether it has any seen
-- before it, of a function Postgres writes into the query that asks: a set-returning SQL function
-- is inlined where a boolean one with a subquery runs once for every row, which for a search
-- matching half a million episodes was most of two seconds.
-- +goose StatementBegin
CREATE FUNCTION seen_before(v viewer, t items) RETURNS SETOF uuid LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT c.id FROM title_groups g JOIN title_groups o ON o.group_id = g.group_id JOIN items c ON c.id = o.item_id
  WHERE g.item_id = t.id AND (c.library_id, c.id) < (t.library_id, t.id) AND sees(v, c)
$$;
-- +goose StatementEnd
DROP FUNCTION first_of_title(viewer, items);

-- +goose Down
CREATE FUNCTION first_of_title(v viewer,
  t items) RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE RETURN NOT EXISTS (
  SELECT 1 FROM title_groups g JOIN title_groups o ON o.group_id = g.group_id JOIN items c ON c.id = o.item_id
  WHERE g.item_id = t.id AND (c.library_id, c.id) < (t.library_id, t.id) AND sees(v, c)
);
DROP FUNCTION seen_before(viewer, items);
