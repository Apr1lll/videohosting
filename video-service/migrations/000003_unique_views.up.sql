-- Уникальные просмотры: каждый зритель засчитывается один раз.
-- viewer_key — "u:<id пользователя>" для вошедших или "a:<хеш IP и браузера>" для гостей.
-- Первичный ключ (video_id, viewer_key) не даёт записать один просмотр дважды.
CREATE TABLE IF NOT EXISTS video_views (
    video_id   BIGINT      NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    viewer_key VARCHAR(80) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (video_id, viewer_key)
);
