// Package repository — слой доступа к данным (PostgreSQL).
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"videohosting/interaction-service/internal/model"
)

var ErrNotFound = errors.New("not found")

type Repository struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// ---------- Комментарии ----------

func (r *Repository) CreateComment(ctx context.Context, c *model.Comment) error {
	return r.db.QueryRow(ctx,
		`INSERT INTO comments (video_id, user_id, username, text)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		c.VideoID, c.UserID, c.Username, c.Text,
	).Scan(&c.ID, &c.CreatedAt)
}

func (r *Repository) GetComment(ctx context.Context, id int64) (*model.Comment, error) {
	var c model.Comment
	err := r.db.QueryRow(ctx,
		`SELECT id, video_id, user_id, username, text, created_at FROM comments WHERE id = $1`, id,
	).Scan(&c.ID, &c.VideoID, &c.UserID, &c.Username, &c.Text, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) ListComments(ctx context.Context, videoID int64, limit, offset int) ([]model.Comment, int, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, video_id, user_id, username, text, created_at, COUNT(*) OVER()
		 FROM comments
		 WHERE video_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		videoID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	comments := make([]model.Comment, 0, limit)
	total := 0
	for rows.Next() {
		var c model.Comment
		if err := rows.Scan(&c.ID, &c.VideoID, &c.UserID, &c.Username, &c.Text, &c.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		comments = append(comments, c)
	}
	return comments, total, rows.Err()
}

func (r *Repository) DeleteComment(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM comments WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Реакции (лайки/дизлайки) ----------

// GetReaction возвращает реакцию пользователя на видео: 1, -1 или 0 (нет реакции).
func (r *Repository) GetReaction(ctx context.Context, videoID, userID int64) (int16, error) {
	var value int16
	err := r.db.QueryRow(ctx,
		`SELECT value FROM reactions WHERE video_id = $1 AND user_id = $2`, videoID, userID,
	).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return value, err
}

// SetReaction ставит или меняет реакцию (UPSERT).
func (r *Repository) SetReaction(ctx context.Context, videoID, userID int64, value int16) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO reactions (video_id, user_id, value)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (video_id, user_id) DO UPDATE SET value = EXCLUDED.value, created_at = NOW()`,
		videoID, userID, value,
	)
	return err
}

func (r *Repository) RemoveReaction(ctx context.Context, videoID, userID int64) error {
	_, err := r.db.Exec(ctx, `DELETE FROM reactions WHERE video_id = $1 AND user_id = $2`, videoID, userID)
	return err
}

func (r *Repository) CountReactions(ctx context.Context, videoID int64) (likes, dislikes int64, err error) {
	err = r.db.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE value = 1),
		        COUNT(*) FILTER (WHERE value = -1)
		 FROM reactions WHERE video_id = $1`, videoID,
	).Scan(&likes, &dislikes)
	return likes, dislikes, err
}

// ---------- Подписки ----------

func (r *Repository) Subscribe(ctx context.Context, subscriberID, channelID int64, channelName string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO subscriptions (subscriber_id, channel_id, channel_name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (subscriber_id, channel_id) DO NOTHING`,
		subscriberID, channelID, channelName,
	)
	return err
}

func (r *Repository) Unsubscribe(ctx context.Context, subscriberID, channelID int64) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM subscriptions WHERE subscriber_id = $1 AND channel_id = $2`, subscriberID, channelID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListSubscriptions(ctx context.Context, subscriberID int64) ([]model.Subscription, error) {
	rows, err := r.db.Query(ctx,
		`SELECT channel_id, channel_name, created_at
		 FROM subscriptions WHERE subscriber_id = $1
		 ORDER BY created_at DESC`, subscriberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	subs := make([]model.Subscription, 0)
	for rows.Next() {
		var s model.Subscription
		if err := rows.Scan(&s.ChannelID, &s.ChannelName, &s.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

func (r *Repository) CountSubscribers(ctx context.Context, channelID int64) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions WHERE channel_id = $1`, channelID).Scan(&n)
	return n, err
}

// DeleteVideoData удаляет все комментарии и реакции видео в одной транзакции.
func (r *Repository) DeleteVideoData(ctx context.Context, videoID int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // после Commit ничего не делает

	if _, err := tx.Exec(ctx, `DELETE FROM comments WHERE video_id = $1`, videoID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM reactions WHERE video_id = $1`, videoID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
