CREATE TABLE IF NOT EXISTS comments (
    id         BIGSERIAL PRIMARY KEY,
    video_id   BIGINT       NOT NULL,
    user_id    BIGINT       NOT NULL,
    username   VARCHAR(32)  NOT NULL DEFAULT '',
    text       VARCHAR(1000) NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS comments_video_id_idx ON comments (video_id, created_at DESC);

-- Одна реакция на видео от одного пользователя: 1 = лайк, -1 = дизлайк.
CREATE TABLE IF NOT EXISTS reactions (
    video_id   BIGINT      NOT NULL,
    user_id    BIGINT      NOT NULL,
    value      SMALLINT    NOT NULL CHECK (value IN (1, -1)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (video_id, user_id)
);

CREATE TABLE IF NOT EXISTS subscriptions (
    subscriber_id BIGINT      NOT NULL,
    channel_id    BIGINT      NOT NULL,
    channel_name  VARCHAR(32) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (subscriber_id, channel_id),
    CHECK (subscriber_id <> channel_id)
);
CREATE INDEX IF NOT EXISTS subscriptions_channel_id_idx ON subscriptions (channel_id);
