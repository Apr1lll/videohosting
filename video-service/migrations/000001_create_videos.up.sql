CREATE TABLE IF NOT EXISTS videos (
    id           BIGSERIAL PRIMARY KEY,
    author_id    BIGINT       NOT NULL,
    author_name  VARCHAR(32)  NOT NULL DEFAULT '',
    title        VARCHAR(200) NOT NULL,
    description  TEXT         NOT NULL DEFAULT '',
    file_name    VARCHAR(64)  NOT NULL UNIQUE,
    content_type VARCHAR(64)  NOT NULL,
    size_bytes   BIGINT       NOT NULL CHECK (size_bytes > 0),
    views        BIGINT       NOT NULL DEFAULT 0 CHECK (views >= 0),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS videos_author_id_idx  ON videos (author_id);
CREATE INDEX IF NOT EXISTS videos_created_at_idx ON videos (created_at DESC);
CREATE INDEX IF NOT EXISTS videos_views_idx      ON videos (views DESC);
