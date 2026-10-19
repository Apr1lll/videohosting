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

const commentColumns = `c.id, c.video_id, c.user_id, c.username, c.text, c.parent_id, c.edited_at, c.created_at,
	(SELECT COUNT(*) FROM comments r WHERE r.parent_id = c.id) AS reply_count`

func (r *Repository) CreateComment(ctx context.Context, c *model.Comment) error {
	return r.db.QueryRow(ctx,
		`INSERT INTO comments (video_id, user_id, username, text, parent_id)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at`,
		c.VideoID, c.UserID, c.Username, c.Text, c.ParentID,
	).Scan(&c.ID, &c.CreatedAt)
}

func (r *Repository) GetComment(ctx context.Context, id int64) (*model.Comment, error) {
	c, err := scanComment(r.db.QueryRow(ctx, `SELECT `+commentColumns+` FROM comments c WHERE c.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// ListComments — комментарии верхнего уровня (новые сверху) с количеством ответов у каждого.
func (r *Repository) ListComments(ctx context.Context, videoID int64, limit, offset int) ([]model.Comment, int, error) {
	return r.queryPage(ctx,
		`SELECT `+commentColumns+`, COUNT(*) OVER()
		 FROM comments c
		 WHERE c.video_id = $1 AND c.parent_id IS NULL
		 ORDER BY c.created_at DESC
		 LIMIT $2 OFFSET $3`, videoID, limit, offset)
}

// ListReplies — ответы на комментарий в хронологическом порядке, как в переписке.
func (r *Repository) ListReplies(ctx context.Context, parentID int64) ([]model.Comment, error) {
	items, _, err := r.queryPage(ctx,
		`SELECT `+commentColumns+`, COUNT(*) OVER()
		 FROM comments c WHERE c.parent_id = $1
		 ORDER BY c.created_at ASC
		 LIMIT 500`, parentID)
	return items, err
}

// AdminListComments — все комментарии сайта для админ-панели, с поиском по тексту и автору.
func (r *Repository) AdminListComments(ctx context.Context, search string, limit, offset int) ([]model.Comment, int, error) {
	return r.queryPage(ctx,
		`SELECT `+commentColumns+`, COUNT(*) OVER()
		 FROM comments c
		 WHERE $1 = '' OR c.text ILIKE '%' || $1 || '%' OR c.username ILIKE '%' || $1 || '%'
		 ORDER BY c.created_at DESC
		 LIMIT $2 OFFSET $3`, search, limit, offset)
}

func (r *Repository) UpdateComment(ctx context.Context, id int64, text string) error {
	tag, err := r.db.Exec(ctx, `UPDATE comments SET text = $2, edited_at = NOW() WHERE id = $1`, id, text)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
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

func (r *Repository) queryPage(ctx context.Context, sql string, args ...any) ([]model.Comment, int, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	comments := make([]model.Comment, 0)
	total := 0
	for rows.Next() {
		var c model.Comment
		if err := rows.Scan(&c.ID, &c.VideoID, &c.UserID, &c.Username, &c.Text, &c.ParentID,
			&c.EditedAt, &c.CreatedAt, &c.ReplyCount, &total); err != nil {
			return nil, 0, err
		}
		comments = append(comments, c)
	}
	return comments, total, rows.Err()
}

func scanComment(row pgx.Row) (*model.Comment, error) {
	var c model.Comment
	err := row.Scan(&c.ID, &c.VideoID, &c.UserID, &c.Username, &c.Text, &c.ParentID,
		&c.EditedAt, &c.CreatedAt, &c.ReplyCount)
	if err != nil {
		return nil, err
	}
	return &c, nil
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

func (r *Repository) Subscribe(ctx context.Context, subscriberID int64, subscriberName string, channelID int64, channelName string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO subscriptions (subscriber_id, subscriber_name, channel_id, channel_name)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (subscriber_id, channel_id) DO NOTHING`,
		subscriberID, subscriberName, channelID, channelName,
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

func (r *Repository) ListSubscribers(ctx context.Context, channelID int64) ([]model.Subscriber, error) {
	rows, err := r.db.Query(ctx,
		`SELECT subscriber_id, subscriber_name, created_at
		 FROM subscriptions WHERE channel_id = $1
		 ORDER BY created_at DESC`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	subs := make([]model.Subscriber, 0)
	for rows.Next() {
		var s model.Subscriber
		if err := rows.Scan(&s.UserID, &s.Username, &s.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// SetSubscriberName дописывает имя подписчика в старые записи, где его не было.
func (r *Repository) SetSubscriberName(ctx context.Context, subscriberID int64, name string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE subscriptions SET subscriber_name = $2 WHERE subscriber_id = $1 AND subscriber_name = ''`,
		subscriberID, name)
	return err
}

func (r *Repository) CountSubscribers(ctx context.Context, channelID int64) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions WHERE channel_id = $1`, channelID).Scan(&n)
	return n, err
}

// ---------- Удаление и статистика ----------

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

// DeleteUserData удаляет всё, что оставил пользователь: комментарии, реакции и подписки
// (в обе стороны). Возвращает ID видео, у которых изменились лайки, чтобы сбросить кеш.
func (r *Repository) DeleteUserData(ctx context.Context, userID int64) ([]int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `DELETE FROM reactions WHERE user_id = $1 RETURNING video_id`, userID)
	if err != nil {
		return nil, err
	}
	videoIDs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}

	for _, q := range []string{
		`DELETE FROM comments WHERE user_id = $1`,
		`DELETE FROM subscriptions WHERE subscriber_id = $1 OR channel_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return nil, err
		}
	}
	return videoIDs, tx.Commit(ctx)
}

func (r *Repository) Stats(ctx context.Context) (*model.Stats, error) {
	var s model.Stats
	err := r.db.QueryRow(ctx,
		`SELECT (SELECT COUNT(*) FROM comments),
		        (SELECT COUNT(*) FROM reactions WHERE value = 1),
		        (SELECT COUNT(*) FROM reactions WHERE value = -1),
		        (SELECT COUNT(*) FROM subscriptions)`,
	).Scan(&s.Comments, &s.Likes, &s.Dislikes, &s.Subscriptions)
	return &s, err
}
