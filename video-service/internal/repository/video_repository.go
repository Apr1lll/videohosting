// Package repository — слой доступа к данным (PostgreSQL).
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"videohosting/video-service/internal/model"
)

var ErrNotFound = errors.New("not found")

const videoColumns = `id, author_id, author_name, title, description, file_name, thumbnail_file,
	content_type, size_bytes, duration_seconds, views, created_at, updated_at`

type VideoRepository struct {
	db *pgxpool.Pool
}

func NewVideoRepository(db *pgxpool.Pool) *VideoRepository {
	return &VideoRepository{db: db}
}

func (r *VideoRepository) Create(ctx context.Context, v *model.Video) error {
	return r.db.QueryRow(ctx,
		`INSERT INTO videos (author_id, author_name, title, description, file_name, thumbnail_file,
		                     content_type, size_bytes, duration_seconds)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, views, created_at, updated_at`,
		v.AuthorID, v.AuthorName, v.Title, v.Description, v.FileName, v.ThumbnailFile,
		v.ContentType, v.SizeBytes, v.DurationSeconds,
	).Scan(&v.ID, &v.Views, &v.CreatedAt, &v.UpdatedAt)
}

func (r *VideoRepository) GetByID(ctx context.Context, id int64) (*model.Video, error) {
	v, err := scanVideo(r.db.QueryRow(ctx, `SELECT `+videoColumns+` FROM videos WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

// List возвращает страницу видео и общее количество найденных записей.
// COUNT(*) OVER() считает общее число строк одним запросом, без отдельного SELECT COUNT.
func (r *VideoRepository) List(ctx context.Context, f model.ListFilter) ([]model.Video, int, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+videoColumns+`, COUNT(*) OVER() AS total
		 FROM videos
		 WHERE ($1 = '' OR title ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%'
		                OR author_name ILIKE '%' || $1 || '%')
		   AND ($2 = 0 OR author_id = $2)
		 ORDER BY created_at DESC
		 LIMIT $3 OFFSET $4`,
		f.Search, f.AuthorID, f.Limit, f.Offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	videos := make([]model.Video, 0, f.Limit)
	total := 0
	for rows.Next() {
		var v model.Video
		if err := rows.Scan(&v.ID, &v.AuthorID, &v.AuthorName, &v.Title, &v.Description, &v.FileName,
			&v.ThumbnailFile, &v.ContentType, &v.SizeBytes, &v.DurationSeconds, &v.Views,
			&v.CreatedAt, &v.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		videos = append(videos, v)
	}
	return videos, total, rows.Err()
}

// Popular возвращает самые просматриваемые видео.
func (r *VideoRepository) Popular(ctx context.Context, limit int) ([]model.Video, error) {
	return r.queryVideos(ctx,
		`SELECT `+videoColumns+` FROM videos ORDER BY views DESC, created_at DESC LIMIT $1`, limit)
}

// Recommendations подбирает видео для боковой колонки в три «яруса», как на YouTube:
//  1. другие видео этого же автора;
//  2. видео на похожую тему — совпадают слова в названии или описании (полнотекстовый поиск);
//  3. все остальные — по популярности.
//
// Внутри яруса «похожих» сортируем по релевантности (ts_rank), иначе — по просмотрам.
func (r *VideoRepository) Recommendations(ctx context.Context, videoID, authorID int64, tsQuery string, limit int) ([]model.Video, error) {
	if tsQuery == "" {
		return r.queryVideos(ctx,
			`SELECT `+videoColumns+` FROM videos
			 WHERE id <> $1
			 ORDER BY (author_id = $2) DESC, views DESC, created_at DESC
			 LIMIT $3`, videoID, authorID, limit)
	}
	return r.queryVideos(ctx,
		`WITH q AS (SELECT to_tsquery('russian', $3) AS query)
		 SELECT `+videoColumns+` FROM videos, q
		 WHERE id <> $1
		 ORDER BY
		   CASE
		     WHEN author_id = $2 THEN 2
		     WHEN to_tsvector('russian', title || ' ' || description) @@ q.query THEN 1
		     ELSE 0
		   END DESC,
		   ts_rank(to_tsvector('russian', title || ' ' || description), q.query) DESC,
		   views DESC,
		   created_at DESC
		 LIMIT $4`, videoID, authorID, tsQuery, limit)
}

func (r *VideoRepository) UpdateInfo(ctx context.Context, id int64, title, description string) (*model.Video, error) {
	v, err := scanVideo(r.db.QueryRow(ctx,
		`UPDATE videos SET title = $2, description = $3, updated_at = NOW()
		 WHERE id = $1 RETURNING `+videoColumns, id, title, description))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func (r *VideoRepository) SetThumbnail(ctx context.Context, id int64, file string) (*model.Video, error) {
	v, err := scanVideo(r.db.QueryRow(ctx,
		`UPDATE videos SET thumbnail_file = $2, updated_at = NOW()
		 WHERE id = $1 RETURNING `+videoColumns, id, file))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func (r *VideoRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM videos WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByAuthor удаляет все видео пользователя и возвращает их, чтобы удалить файлы.
func (r *VideoRepository) DeleteByAuthor(ctx context.Context, authorID int64) ([]model.Video, error) {
	return r.queryVideos(ctx, `DELETE FROM videos WHERE author_id = $1 RETURNING `+videoColumns, authorID)
}

// RecordView запоминает, что зритель посмотрел видео. Возвращает true, если это первый
// просмотр этого зрителя: повторная вставка упирается в первичный ключ и ничего не делает.
func (r *VideoRepository) RecordView(ctx context.Context, videoID int64, viewerKey string) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`INSERT INTO video_views (video_id, viewer_key) VALUES ($1, $2)
		 ON CONFLICT (video_id, viewer_key) DO NOTHING`, videoID, viewerKey)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // нарушение внешнего ключа — видео нет
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// AddViews прибавляет накопленные в Redis просмотры к счётчику в БД.
func (r *VideoRepository) AddViews(ctx context.Context, id, delta int64) error {
	_, err := r.db.Exec(ctx, `UPDATE videos SET views = views + $2 WHERE id = $1`, id, delta)
	return err
}

func (r *VideoRepository) Stats(ctx context.Context) (*model.Stats, error) {
	var s model.Stats
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(SUM(views), 0), COALESCE(SUM(size_bytes), 0), COUNT(DISTINCT author_id)
		 FROM videos`).Scan(&s.Videos, &s.Views, &s.StorageBytes, &s.Authors)
	return &s, err
}

func (r *VideoRepository) queryVideos(ctx context.Context, sql string, args ...any) ([]model.Video, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	videos := make([]model.Video, 0)
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		videos = append(videos, *v)
	}
	return videos, rows.Err()
}

func scanVideo(row pgx.Row) (*model.Video, error) {
	var v model.Video
	err := row.Scan(&v.ID, &v.AuthorID, &v.AuthorName, &v.Title, &v.Description, &v.FileName,
		&v.ThumbnailFile, &v.ContentType, &v.SizeBytes, &v.DurationSeconds, &v.Views,
		&v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
