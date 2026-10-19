DROP INDEX IF EXISTS videos_fts_idx;
ALTER TABLE videos DROP COLUMN IF EXISTS updated_at;
ALTER TABLE videos DROP COLUMN IF EXISTS duration_seconds;
ALTER TABLE videos DROP COLUMN IF EXISTS thumbnail_file;
