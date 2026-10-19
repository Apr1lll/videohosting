// Package service содержит бизнес-логику работы с видео.
package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"videohosting/video-service/internal/model"
	"videohosting/video-service/internal/repository"
)

var (
	ErrNotFound  = errors.New("video not found")
	ErrForbidden = errors.New("you can change only your own videos")
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

const (
	popularLimit      = 10
	maxThumbnailBytes = 5 << 20 // 5 МБ
)

// Интерфейсы зависимостей: сервис не знает о конкретных Postgres/Redis,
// что упрощает тестирование и замену реализаций.
type VideoRepository interface {
	Create(ctx context.Context, v *model.Video) error
	GetByID(ctx context.Context, id int64) (*model.Video, error)
	List(ctx context.Context, f model.ListFilter) ([]model.Video, int, error)
	Popular(ctx context.Context, limit int) ([]model.Video, error)
	Recommendations(ctx context.Context, videoID, authorID int64, tsQuery string, limit int) ([]model.Video, error)
	UpdateInfo(ctx context.Context, id int64, title, description string) (*model.Video, error)
	SetThumbnail(ctx context.Context, id int64, file string) (*model.Video, error)
	Delete(ctx context.Context, id int64) error
	DeleteByAuthor(ctx context.Context, authorID int64) ([]model.Video, error)
	AddViews(ctx context.Context, id, delta int64) error
	Stats(ctx context.Context) (*model.Stats, error)
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
	videos      FileStorage
	thumbs      FileStorage
	interaction InteractionClient
}

func NewVideoService(repo VideoRepository, cache Cache, videos, thumbs FileStorage, interaction InteractionClient) *VideoService {
	return &VideoService{repo: repo, cache: cache, videos: videos, thumbs: thumbs, interaction: interaction}
}

// Actor — кто выполняет действие. Администратор может менять любые видео.
type Actor struct {
	UserID  int64
	IsAdmin bool
}

func (a Actor) canEdit(v *model.Video) bool {
	return a.IsAdmin || v.AuthorID == a.UserID
}

// ---------- Загрузка ----------

// StoredFile — видеофайл, уже записанный в хранилище, но ещё не сохранённый в БД.
type StoredFile struct {
	Name        string
	Size        int64
	ContentType string
}

// SaveVideoFile потоково пишет видео прямо в хранилище — без промежуточных временных файлов.
// Формат проверяется по расширению до начала записи.
func (s *VideoService) SaveVideoFile(src io.Reader, originalName string) (*StoredFile, error) {
	ext := strings.ToLower(filepath.Ext(originalName))
	contentType, ok := allowedFormats[ext]
	if !ok {
		return nil, &ValidationError{"unsupported file format, allowed: mp4, webm, mov, mkv"}
	}
	name, size, err := s.videos.Save(src, ext)
	if err != nil {
		return nil, fmt.Errorf("save file: %w", err)
	}
	if size == 0 {
		_ = s.videos.Remove(name)
		return nil, &ValidationError{"file is empty"}
	}
	return &StoredFile{Name: name, Size: size, ContentType: contentType}, nil
}

// DiscardVideoFile удаляет файл, если загрузку пришлось прервать.
func (s *VideoService) DiscardVideoFile(f *StoredFile) {
	if f != nil {
		_ = s.videos.Remove(f.Name)
	}
}

type CreateInput struct {
	AuthorID        int64
	AuthorName      string
	Title           string
	Description     string
	DurationSeconds int
	File            *StoredFile
	Thumbnail       []byte // необязательно: кадр, который браузер вырезал из видео
}

func (s *VideoService) Create(ctx context.Context, in CreateInput) (*model.Video, error) {
	if in.File == nil {
		return nil, &ValidationError{"field 'file' is required"}
	}
	title, description, err := validateInfo(in.Title, in.Description)
	if err != nil {
		s.DiscardVideoFile(in.File)
		return nil, err
	}

	var thumbName string
	if len(in.Thumbnail) > 0 {
		if thumbName, err = s.saveThumbnail(in.Thumbnail); err != nil {
			s.DiscardVideoFile(in.File)
			return nil, err
		}
	}

	video := &model.Video{
		AuthorID:        in.AuthorID,
		AuthorName:      in.AuthorName,
		Title:           title,
		Description:     description,
		FileName:        in.File.Name,
		ThumbnailFile:   thumbName,
		ContentType:     in.File.ContentType,
		SizeBytes:       in.File.Size,
		DurationSeconds: max(0, min(in.DurationSeconds, 24*3600)),
	}
	if err := s.repo.Create(ctx, video); err != nil {
		// Запись в БД не удалась — удаляем файлы, чтобы не копить «мусор» на диске.
		s.DiscardVideoFile(in.File)
		s.removeThumb(thumbName)
		return nil, fmt.Errorf("create video: %w", err)
	}
	video.FillURLs()
	return video, nil
}

func validateInfo(title, description string) (string, string, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if n := utf8.RuneCountInString(title); n == 0 || n > 200 {
		return "", "", &ValidationError{"title is required and must be at most 200 characters"}
	}
	if utf8.RuneCountInString(description) > 5000 {
		return "", "", &ValidationError{"description must be at most 5000 characters"}
	}
	return title, description, nil
}

// ---------- Просмотр ----------

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
	video.FillURLs()
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
	fillAll(videos)
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
	fillAll(videos)
	if err := s.cache.SetPopular(ctx, videos); err != nil {
		slog.Warn("cache write failed", "key", "popular", "err", err)
	}
	return videos, nil
}

// Recommendations — видео для колонки справа: сначала этого автора, потом похожие, потом остальные.
func (s *VideoService) Recommendations(ctx context.Context, id int64, limit int) ([]model.Video, error) {
	video, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	query := buildTSQuery(video.Title + " " + video.Description)
	videos, err := s.repo.Recommendations(ctx, id, video.AuthorID, query, limit)
	if err != nil {
		return nil, err
	}
	fillAll(videos)
	return videos, nil
}

// buildTSQuery превращает текст в запрос вида "слово1 | слово2 | ...":
// видео считается похожим, если совпадает хотя бы одно значимое слово.
// В запрос попадают только буквы и цифры, поэтому спецсимволы tsquery внедрить нельзя.
func buildTSQuery(text string) string {
	seen := make(map[string]bool)
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if utf8.RuneCountInString(w) < 2 || seen[w] {
			continue
		}
		seen[w] = true
		words = append(words, w)
		if len(words) == 20 {
			break
		}
	}
	return strings.Join(words, " | ")
}

// ---------- Редактирование ----------

func (s *VideoService) UpdateInfo(ctx context.Context, actor Actor, id int64, title, description string) (*model.Video, error) {
	if _, err := s.editable(ctx, actor, id); err != nil {
		return nil, err
	}
	title, description, err := validateInfo(title, description)
	if err != nil {
		return nil, err
	}
	video, err := s.repo.UpdateInfo(ctx, id, title, description)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id)
	video.FillURLs()
	return video, nil
}

// SetThumbnail заменяет превью видео (JPEG, PNG или WebP до 5 МБ).
func (s *VideoService) SetThumbnail(ctx context.Context, actor Actor, id int64, data []byte) (*model.Video, error) {
	old, err := s.editable(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	name, err := s.saveThumbnail(data)
	if err != nil {
		return nil, err
	}
	video, err := s.repo.SetThumbnail(ctx, id, name)
	if err != nil {
		s.removeThumb(name)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.removeThumb(old.ThumbnailFile)
	s.invalidate(ctx, id)
	video.FillURLs()
	return video, nil
}

func (s *VideoService) OpenThumbnail(ctx context.Context, id int64) (*model.Video, *os.File, error) {
	video, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if video.ThumbnailFile == "" {
		return nil, nil, ErrNotFound
	}
	f, err := s.thumbs.Open(video.ThumbnailFile)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	return video, f, nil
}

func (s *VideoService) saveThumbnail(data []byte) (string, error) {
	if len(data) > maxThumbnailBytes {
		return "", &ValidationError{"thumbnail must be at most 5 MB"}
	}
	var ext string
	switch http.DetectContentType(data) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	default:
		return "", &ValidationError{"thumbnail must be a JPEG, PNG or WebP image"}
	}
	name, _, err := s.thumbs.Save(bytes.NewReader(data), ext)
	if err != nil {
		return "", fmt.Errorf("save thumbnail: %w", err)
	}
	return name, nil
}

// ---------- Удаление ----------

func (s *VideoService) Delete(ctx context.Context, actor Actor, id int64) error {
	video, err := s.editable(ctx, actor, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.cleanupDeleted(ctx, *video)
	return nil
}

// DeleteUserVideos удаляет все видео пользователя (вызывается auth-service при удалении аккаунта).
func (s *VideoService) DeleteUserVideos(ctx context.Context, authorID int64) (int, error) {
	videos, err := s.repo.DeleteByAuthor(ctx, authorID)
	if err != nil {
		return 0, err
	}
	for _, v := range videos {
		s.cleanupDeleted(ctx, v)
	}
	return len(videos), nil
}

// cleanupDeleted убирает всё, что связано с удалённым видео: файлы, кеш, комментарии и лайки.
func (s *VideoService) cleanupDeleted(ctx context.Context, v model.Video) {
	if err := s.videos.Remove(v.FileName); err != nil {
		slog.Warn("failed to remove video file", "file", v.FileName, "err", err)
	}
	s.removeThumb(v.ThumbnailFile)
	s.invalidate(ctx, v.ID)

	// Комментарии и лайки хранятся в другом сервисе — просим его удалить их.
	// Ошибка здесь не критична для пользователя, поэтому только логируем её.
	if err := s.interaction.DeleteVideoData(ctx, v.ID); err != nil {
		slog.Warn("failed to delete video interactions", "video_id", v.ID, "err", err)
	}
}

func (s *VideoService) AdminStats(ctx context.Context) (*model.Stats, error) {
	return s.repo.Stats(ctx)
}

// ---------- Видеофайл и просмотры ----------

// OpenStream открывает файл видео для отдачи клиенту. Файл закрывает вызывающий код.
func (s *VideoService) OpenStream(ctx context.Context, id int64) (*model.Video, *os.File, error) {
	video, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.videos.Open(video.FileName)
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

// ---------- Вспомогательное ----------

// editable загружает видео из БД и проверяет, что actor может его менять.
func (s *VideoService) editable(ctx context.Context, actor Actor, id int64) (*model.Video, error) {
	video, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !actor.canEdit(video) {
		return nil, ErrForbidden
	}
	return video, nil
}

func (s *VideoService) invalidate(ctx context.Context, id int64) {
	_ = s.cache.DeleteVideo(ctx, id)
	_ = s.cache.InvalidatePopular(ctx)
}

func (s *VideoService) removeThumb(name string) {
	if name == "" {
		return
	}
	if err := s.thumbs.Remove(name); err != nil {
		slog.Warn("failed to remove thumbnail", "file", name, "err", err)
	}
}

func fillAll(videos []model.Video) {
	for i := range videos {
		videos[i].FillURLs()
	}
}
