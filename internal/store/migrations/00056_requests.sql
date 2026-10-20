-- +goose Up
-- A profile asks for a title a library of files' search found that no library holds. The request
-- waits for an admin, or the manager that keeps the profile, to approve or decline it; an admin's
-- own is approved as it is asked. A title is asked of a library once while its request is not
-- declined.
CREATE TABLE requests (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  kind text NOT NULL CONSTRAINT request_kind CHECK (kind IN ('movie', 'show')),
  provider text NOT NULL
    CONSTRAINT request_provider CHECK (provider IN ('tmdb', 'imdb', 'tvdb', 'anidb', 'anilist') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  value text NOT NULL,
  title text NOT NULL,
  year int,
  -- seasons are a show's asked for, every one where null.
  seasons int[] CONSTRAINT request_seasons CHECK (seasons IS NULL OR kind = 'show'),
  state text NOT NULL DEFAULT 'pending' CONSTRAINT request_state CHECK (state IN ('pending', 'approved', 'declined')),
  reason text,
  decided_by uuid REFERENCES profiles(id) ON DELETE SET NULL,
  asked_at timestamptz NOT NULL DEFAULT now(),
  decided_at timestamptz,
  CONSTRAINT request_decided CHECK ((state = 'pending') = (decided_at IS NULL))
);
CREATE UNIQUE INDEX requests_open ON requests (library_id, provider, value) WHERE state <> 'declined';
CREATE INDEX requests_profile ON requests (profile_id, asked_at DESC, id DESC);
CREATE INDEX requests_asked ON requests (asked_at DESC, id DESC);
CREATE INDEX requests_decided_by ON requests (decided_by);

-- +goose Down
DROP TABLE requests;
