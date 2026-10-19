ALTER TABLE videos ADD COLUMN IF NOT EXISTS thumbnail_file   VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE videos ADD COLUMN IF NOT EXISTS duration_seconds INTEGER     NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0);
ALTER TABLE videos ADD COLUMN IF NOT EXISTS updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Полнотекстовый индекс для рекомендаций «на похожую тему».
-- Словарь 'russian' приводит слова к основе: «котики», «котиков» и «котик» считаются одним словом.
CREATE INDEX IF NOT EXISTS videos_fts_idx
    ON videos USING GIN (to_tsvector('russian', title || ' ' || description));
