package model

import "time"

type Comment struct {
	ID         int64      `json:"id"`
	VideoID    int64      `json:"video_id"`
	UserID     int64      `json:"user_id"`
	Username   string     `json:"username"`
	Text       string     `json:"text"`
	ParentID   *int64     `json:"parent_id,omitempty"` // nil — комментарий верхнего уровня
	ReplyCount int        `json:"reply_count"`
	EditedAt   *time.Time `json:"edited_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CommentPage struct {
	Items []Comment `json:"items"`
	Total int       `json:"total"`
	Page  int       `json:"page"`
	Limit int       `json:"limit"`
}

// Значения реакции в БД.
const (
	ReactionLike    int16 = 1
	ReactionDislike int16 = -1
)

type ReactionStats struct {
	VideoID    int64  `json:"video_id"`
	Likes      int64  `json:"likes"`
	Dislikes   int64  `json:"dislikes"`
	MyReaction string `json:"my_reaction,omitempty"` // "like", "dislike" или пусто
}

// Subscription — канал, на который подписан пользователь.
type Subscription struct {
	ChannelID   int64     `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// Subscriber — пользователь, подписанный на канал.
type Subscriber struct {
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// Stats — сводка для админ-панели.
type Stats struct {
	Comments      int64 `json:"comments"`
	Likes         int64 `json:"likes"`
	Dislikes      int64 `json:"dislikes"`
	Subscriptions int64 `json:"subscriptions"`
}
