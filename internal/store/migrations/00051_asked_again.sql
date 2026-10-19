-- +goose NO TRANSACTION
-- +goose Up
-- When an episode was asked to be described again by its show's next match, its fields kept by the
-- sources that said them until those sources answer. A column with no default rewrites nothing.
ALTER TABLE items ADD COLUMN asked_again_at timestamptz;

-- +goose Down
ALTER TABLE items DROP COLUMN asked_again_at;
