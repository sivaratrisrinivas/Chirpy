-- +goose Up
CREATE INDEX IF NOT EXISTS idx_chirps_created_id ON chirps (created_at, id);
CREATE INDEX IF NOT EXISTS idx_chirps_user_created ON chirps (user_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens (user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_refresh_tokens_user;
DROP INDEX IF EXISTS idx_chirps_user_created;
DROP INDEX IF EXISTS idx_chirps_created_id;
