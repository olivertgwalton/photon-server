-- +goose NO TRANSACTION
-- +goose Up
-- Checks the sessions there are against 00006's constraints, reading the table without stopping its
-- writes, and finds a provider account's sessions without reading them all.
ALTER TABLE device_sessions VALIDATE CONSTRAINT device_session_sign_in;
ALTER TABLE device_sessions VALIDATE CONSTRAINT device_session_sign_in_whole;
CREATE INDEX CONCURRENTLY IF NOT EXISTS device_sessions_sign_in ON device_sessions (sign_in_provider, sign_in_subject)
  WHERE sign_in_provider IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS device_sessions_sign_in;
