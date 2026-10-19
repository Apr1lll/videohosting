// Package repository — слой доступа к данным (PostgreSQL).
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"videohosting/video-service/internal/model"
)

var ErrNotFound = errors.New("not found")

const videoColumns = `id, author_id, author_name, title, description, file_name,
	content_type, size_bytes, views, created_at`

type VideoRepository struct {
	db *pgxpool.Pool
}

func NewVideoRepository(db *pgxpool.Pool) *VideoRepository {
	return &VideoRepository{db: db}
}

func (r *VideoRepository) Create(ctx context.Context, v *model.Video) error {
	return r.db.QueryRow(ctx,
		`INSERT INTO videos (author_id, author_name, title, description, file_name, content_type, size_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, views, created_at`,
		v.AuthorID, v.AuthorName, v.Title, v.Description, v.FileName, v.ContentType, v.SizeBytes,
	).Scan(&v.ID, &v.Views, &v.CreatedAt)
}

func (r *VideoRepository) GetByID(ctx context.Context, id int64) (*model.Video, error) {
	row := r.db.QueryRow(ctx, `SELECT `+videoColumns+` FROM videos WHERE id = $1`, id)
	v, err := scanVideo(row)
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
		 WHERE ($1 = '' OR title ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%')
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
			&v.ContentType, &v.SizeBytes, &v.Views, &v.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		videos = append(videos, v)
	}
	return videos, total, rows.Err()
}

// Popular возвращает самые просматриваемые видео.
func (r *VideoRepository) Popular(ctx context.Context, limit int) ([]model.Video, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+videoColumns+` FROM videos ORDER BY views DESC, created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	videos := make([]model.Video, 0, limit)
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		videos = append(videos, *v)
	}
	return videos, rows.Err()
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

// AddViews прибавляет накопленные в Redis просмотры к счётчику в БД.
func (r *VideoRepository) AddViews(ctx context.Context, id, delta int64) error {
	_, err := r.db.Exec(ctx, `UPDATE videos SET views = views + $2 WHERE id = $1`, id, delta)
	return err
}

func scanVideo(row pgx.Row) (*model.Video, error) {
	var v model.Video
	err := row.Scan(&v.ID, &v.AuthorID, &v.AuthorName, &v.Title, &v.Description, &v.FileName,
		&v.ContentType, &v.SizeBytes, &v.Views, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
