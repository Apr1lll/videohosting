DROP INDEX IF EXISTS reactions_user_id_idx;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS subscriber_name;
DROP INDEX IF EXISTS comments_user_id_idx;
DROP INDEX IF EXISTS comments_parent_id_idx;
ALTER TABLE comments DROP COLUMN IF EXISTS edited_at;
ALTER TABLE comments DROP COLUMN IF EXISTS parent_id;
