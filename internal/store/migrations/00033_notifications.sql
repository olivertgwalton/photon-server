-- +goose Up
-- A profile is told of the films and episodes a scan adds that it cares for, as its notify says. A
-- show's new episodes gather under one notification while it is unread; each film or episode is
-- told to a profile once, whatever becomes of its notification.
ALTER TABLE profile_preferences ADD COLUMN notify text NOT NULL DEFAULT 'marked'
  CONSTRAINT notify CHECK (notify IN ('marked', 'all', 'none'));

-- A library a profile turned quiet tells it nothing; one added later tells it, as the rest do.
CREATE TABLE quiet_libraries (
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  library_id uuid NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  PRIMARY KEY (profile_id, library_id)
);
CREATE INDEX quiet_libraries_library ON quiet_libraries (library_id);

CREATE TABLE notifications (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  title_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  added_at timestamptz NOT NULL DEFAULT now(),
  read_at timestamptz
);
CREATE UNIQUE INDEX notifications_unread ON notifications (profile_id, title_id) WHERE read_at IS NULL;
CREATE INDEX notifications_profile ON notifications (profile_id, added_at DESC, id DESC);
CREATE INDEX notifications_title ON notifications (title_id);

CREATE TABLE notification_items (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  profile_id uuid NOT NULL,
  notification_id bigint NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
  PRIMARY KEY (item_id, profile_id)
);
CREATE INDEX notification_items_notification ON notification_items (notification_id);

-- +goose Down
DROP TABLE notification_items;
DROP TABLE notifications;
DROP TABLE quiet_libraries;
ALTER TABLE profile_preferences DROP COLUMN notify;
