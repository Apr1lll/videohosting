package model

import "time"

type Video struct {
	ID          int64     `json:"id"`
	AuthorID    int64     `json:"author_id"`
	AuthorName  string    `json:"author_name"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	FileName    string    `json:"-"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	Views       int64     `json:"views"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListFilter — параметры поиска и пагинации для списка видео.
type ListFilter struct {
	Search   string
	AuthorID int64
	Limit    int
	Offset   int
}

type VideoPage struct {
	Items []Video `json:"items"`
	Total int     `json:"total"`
	Page  int     `json:"page"`
	Limit int     `json:"limit"`
}
