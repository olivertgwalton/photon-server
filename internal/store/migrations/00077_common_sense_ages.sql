-- +goose Up
-- The age Common Sense Media advises a title for, as each source that carries it said it, kept
-- beside the source's ratings and replaced with them.
CREATE TABLE common_sense_ages (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL
    CONSTRAINT common_sense_age_source CHECK (source ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$'),
  age smallint NOT NULL CHECK (age BETWEEN 1 AND 18),
  PRIMARY KEY (item_id, source)
);

-- +goose Down
DROP TABLE common_sense_ages;
