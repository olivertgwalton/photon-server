-- +goose Up
-- People, as providers know them: one row per person whatever titles credit them. A person's
-- picture is served by photo_id, which changes whenever the picture does.
CREATE TABLE people (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  name text NOT NULL,
  tmdb_id text UNIQUE,
  photo_url text,
  photo_id uuid UNIQUE,
  biography text,
  born date,
  died date,
  birthplace text,
  described_at timestamptz
);
CREATE INDEX people_name ON people (search_text(name) text_pattern_ops);

-- What each source credits a person with on a title, in its order.
CREATE TABLE credits (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  source text NOT NULL CONSTRAINT credit_source CHECK (source IN ('nfo', 'tmdb', 'tvdb')),
  kind text NOT NULL CONSTRAINT credit_kind CHECK (kind IN ('actor', 'guest_star', 'director', 'writer',
    'producer', 'composer', 'creator')),
  role text NOT NULL DEFAULT '',
  position int NOT NULL,
  PRIMARY KEY (item_id, source, kind, person_id, role)
);
CREATE INDEX credits_person ON credits (person_id);

-- +goose Down
DROP TABLE credits, people;
