-- Роль пользователя ('user' или 'admin') и файл аватарки.
ALTER TABLE users ADD COLUMN IF NOT EXISTS role        VARCHAR(16) NOT NULL DEFAULT 'user';
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_file VARCHAR(64) NOT NULL DEFAULT '';
