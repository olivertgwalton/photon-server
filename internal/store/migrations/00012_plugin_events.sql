-- +goose NO TRANSACTION
-- +goose Up
-- A plugin is told of the events it hears as a webhook of its own is, which goes with it.
ALTER TABLE webhooks ADD COLUMN plugin text;
ALTER TABLE webhooks ADD CONSTRAINT webhook_plugin FOREIGN KEY (plugin) REFERENCES plugins(slug) ON DELETE CASCADE NOT VALID;
ALTER TABLE webhooks VALIDATE CONSTRAINT webhook_plugin;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS webhooks_plugin ON webhooks (plugin);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS webhooks_plugin;
DELETE FROM webhooks WHERE plugin IS NOT NULL;
ALTER TABLE webhooks DROP COLUMN plugin;
