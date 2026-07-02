-- +goose Up
-- +goose StatementBegin
-- Add composite indexes to speed up per-session ordered lookups.
CREATE INDEX IF NOT EXISTS idx_messages_session_created ON messages (session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_files_session_version_created ON files (session_id, version, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_messages_session_created;
DROP INDEX IF EXISTS idx_files_session_version_created;
-- +goose StatementEnd
