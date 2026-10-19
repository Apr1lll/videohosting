package model

import (
	"fmt"
	"time"
)

type Video struct {
	ID              int64     `json:"id"`
	AuthorID        int64     `json:"author_id"`
	AuthorName      string    `json:"author_name"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	FileName        string    `json:"-"`
	ThumbnailFile   string    `json:"-"`
	ThumbnailURL    string    `json:"thumbnail_url"`
	ContentType     string    `json:"content_type"`
	SizeBytes       int64     `json:"size_bytes"`
	DurationSeconds int       `json:"duration_seconds"`
	Views           int64     `json:"views"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// FillURLs проставляет ссылку на превью. Параметр v меняется при замене превью,
// поэтому браузер может кешировать картинку надолго и при этом не покажет устаревшую.
func (v *Video) FillURLs() {
	v.ThumbnailURL = ""
	if v.ThumbnailFile != "" {
		v.ThumbnailURL = fmt.Sprintf("/videos/%d/thumbnail?v=%d", v.ID, v.UpdatedAt.Unix())
	}
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

// Stats — сводка для админ-панели.
type Stats struct {
	Videos       int64 `json:"videos"`
	Views        int64 `json:"views"`
	StorageBytes int64 `json:"storage_bytes"`
	Authors      int64 `json:"authors"`
}
