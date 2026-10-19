-- Ответы на комментарии: parent_id указывает на корневой комментарий.
-- При удалении комментария ответы на него удаляются автоматически.
ALTER TABLE comments ADD COLUMN IF NOT EXISTS parent_id BIGINT REFERENCES comments(id) ON DELETE CASCADE;
ALTER TABLE comments ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS comments_parent_id_idx ON comments (parent_id, created_at);
CREATE INDEX IF NOT EXISTS comments_user_id_idx   ON comments (user_id);

-- Имя подписчика, чтобы автор канала видел, кто на него подписан.
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS subscriber_name VARCHAR(32) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS reactions_user_id_idx ON reactions (user_id);
