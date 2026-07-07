-- +goose Up
-- +goose StatementBegin
-- Add indexes for hot-path queries that previously required full table scans.
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions (updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_role_created ON messages (role, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_files_path_version ON files (path, version DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_sessions_updated_at;
DROP INDEX IF EXISTS idx_messages_role_created;
DROP INDEX IF EXISTS idx_files_path_version;
-- +goose StatementEnd
