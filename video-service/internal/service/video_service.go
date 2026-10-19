// Package service содержит бизнес-логику работы с видео.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"videohosting/video-service/internal/model"
	"videohosting/video-service/internal/repository"
)

var (
	ErrNotFound  = errors.New("video not found")
	ErrForbidden = errors.New("you can delete only your own videos")
)

type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

// Поддерживаемые форматы: расширение файла -> MIME-тип.
var allowedFormats = map[string]string{
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".mkv":  "video/x-matroska",
}

const popularLimit = 10

// Интерфейсы зависимостей: сервис не знает о конкретных Postgres/Redis,
// что упрощает тестирование и замену реализаций.
type VideoRepository interface {
	Create(ctx context.Context, v *model.Video) error
	GetByID(ctx context.Context, id int64) (*model.Video, error)
	List(ctx context.Context, f model.ListFilter) ([]model.Video, int, error)
	Popular(ctx context.Context, limit int) ([]model.Video, error)
	Delete(ctx context.Context, id int64) error
	AddViews(ctx context.Context, id, delta int64) error
}

type Cache interface {
	GetVideo(ctx context.Context, id int64) (*model.Video, error)
	SetVideo(ctx context.Context, v *model.Video) error
	DeleteVideo(ctx context.Context, id int64) error
	IncrView(ctx context.Context, id int64) error
	AddPendingViews(ctx context.Context, id, n int64) error
	PendingViews(ctx context.Context, id int64) (int64, error)
	PopPendingViews(ctx context.Context) (map[int64]int64, error)
	GetPopular(ctx context.Context) ([]model.Video, error)
	SetPopular(ctx context.Context, videos []model.Video) error
	InvalidatePopular(ctx context.Context) error
}

type FileStorage interface {
	Save(src io.Reader, ext string) (string, int64, error)
	Open(name string) (*os.File, error)
	Remove(name string) error
}

type InteractionClient interface {
	DeleteVideoData(ctx context.Context, videoID int64) error
}

type VideoService struct {
	repo        VideoRepository
	cache       Cache
	storage     FileStorage
	interaction InteractionClient
}

func NewVideoService(repo VideoRepository, cache Cache, storage FileStorage, interaction InteractionClient) *VideoService {
	return &VideoService{repo: repo, cache: cache, storage: storage, interaction: interaction}
}

type UploadInput struct {
	AuthorID    int64
	AuthorName  string
	Title       string
	Description string
	FileName    string // исходное имя файла от клиента, нужно только для расширения
	File        io.Reader
}

func (s *VideoService) Upload(ctx context.Context, in UploadInput) (*model.Video, error) {
	title := strings.TrimSpace(in.Title)
	description := strings.TrimSpace(in.Description)

	if n := utf8.RuneCountInString(title); n == 0 || n > 200 {
		return nil, &ValidationError{"title is required and must be at most 200 characters"}
	}
	if utf8.RuneCountInString(description) > 5000 {
		return nil, &ValidationError{"description must be at most 5000 characters"}
	}

	ext := strings.ToLower(filepath.Ext(in.FileName))
	contentType, ok := allowedFormats[ext]
	if !ok {
		return nil, &ValidationError{"unsupported file format, allowed: mp4, webm, mov, mkv"}
	}

	fileName, size, err := s.storage.Save(in.File, ext)
	if err != nil {
		return nil, fmt.Errorf("save file: %w", err)
	}
	if size == 0 {
		_ = s.storage.Remove(fileName)
		return nil, &ValidationError{"file is empty"}
	}

	video := &model.Video{
		AuthorID:    in.AuthorID,
		AuthorName:  in.AuthorName,
		Title:       title,
		Description: description,
		FileName:    fileName,
		ContentType: contentType,
		SizeBytes:   size,
	}
	if err := s.repo.Create(ctx, video); err != nil {
		// Запись в БД не удалась — удаляем файл, чтобы не копить «мусор» на диске.
		_ = s.storage.Remove(fileName)
		return nil, fmt.Errorf("create video: %w", err)
	}
	return video, nil
}

// Get возвращает карточку видео: сначала ищет в Redis, при промахе идёт в БД.
func (s *VideoService) Get(ctx context.Context, id int64) (*model.Video, error) {
	video, err := s.cache.GetVideo(ctx, id)
	if err != nil {
		slog.Warn("cache read failed", "video_id", id, "err", err)
	}

	if video == nil {
		video, err = s.repo.GetByID(ctx, id)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if err := s.cache.SetVideo(ctx, video); err != nil {
			slog.Warn("cache write failed", "video_id", id, "err", err)
		}
	}

	// Добавляем просмотры, которые ещё лежат в Redis и не сохранены в БД.
	if pending, err := s.cache.PendingViews(ctx, id); err == nil {
		video.Views += pending
	}
	return video, nil
}

func (s *VideoService) List(ctx context.Context, search string, authorID int64, page, limit int) (*model.VideoPage, error) {
	filter := model.ListFilter{
		Search:   strings.TrimSpace(search),
		AuthorID: authorID,
		Limit:    limit,
		Offset:   (page - 1) * limit,
	}
	videos, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return &model.VideoPage{Items: videos, Total: total, Page: page, Limit: limit}, nil
}

func (s *VideoService) Popular(ctx context.Context) ([]model.Video, error) {
	if videos, err := s.cache.GetPopular(ctx); err == nil && videos != nil {
		return videos, nil
	}

	videos, err := s.repo.Popular(ctx, popularLimit)
	if err != nil {
		return nil, err
	}
	if err := s.cache.SetPopular(ctx, videos); err != nil {
		slog.Warn("cache write failed", "key", "popular", "err", err)
	}
	return videos, nil
}

func (s *VideoService) Delete(ctx context.Context, id, userID int64) error {
	video, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if video.AuthorID != userID {
		return ErrForbidden
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.storage.Remove(video.FileName); err != nil {
		slog.Warn("failed to remove video file", "file", video.FileName, "err", err)
	}
	_ = s.cache.DeleteVideo(ctx, id)
	_ = s.cache.InvalidatePopular(ctx)

	// Комментарии и лайки хранятся в другом сервисе — просим его удалить их.
	// Ошибка здесь не критична для пользователя, поэтому только логируем её.
	if err := s.interaction.DeleteVideoData(ctx, id); err != nil {
		slog.Warn("failed to delete video interactions", "video_id", id, "err", err)
	}
	return nil
}

// OpenStream открывает файл видео для отдачи клиенту. Файл закрывает вызывающий код.
func (s *VideoService) OpenStream(ctx context.Context, id int64) (*model.Video, *os.File, error) {
	video, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.storage.Open(video.FileName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	return video, f, nil
}

func (s *VideoService) RegisterView(ctx context.Context, id int64) {
	if err := s.cache.IncrView(ctx, id); err != nil {
		slog.Warn("failed to register view", "video_id", id, "err", err)
	}
}

// RunViewsFlusher периодически переносит просмотры из Redis в Postgres.
// Работает до отмены контекста, при остановке делает последний сброс.
func (s *VideoService) RunViewsFlusher(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			s.flushViews(flushCtx)
			cancel()
			return
		case <-ticker.C:
			s.flushViews(ctx)
		}
	}
}

func (s *VideoService) flushViews(ctx context.Context) {
	views, err := s.cache.PopPendingViews(ctx)
	if err != nil {
		slog.Error("failed to read pending views", "err", err)
	}

	for id, n := range views {
		if err := s.repo.AddViews(ctx, id, n); err != nil {
			slog.Error("failed to save views, returning them to redis", "video_id", id, "err", err)
			_ = s.cache.AddPendingViews(ctx, id, n)
			continue
		}
		// Карточка в кеше теперь устарела — сбрасываем её.
		_ = s.cache.DeleteVideo(ctx, id)
	}
	if len(views) > 0 {
		slog.Info("views flushed to database", "videos", len(views))
	}
}
