// Package service содержит бизнес-логику комментариев, реакций и подписок.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"videohosting/interaction-service/internal/model"
	"videohosting/interaction-service/internal/repository"
)

var (
	ErrVideoNotFound   = errors.New("video not found")
	ErrCommentNotFound = errors.New("comment not found")
	ErrChannelNotFound = errors.New("channel not found")
	ErrNotSubscribed   = errors.New("you are not subscribed to this channel")
	ErrForbidden       = errors.New("you can change only your own comments")
)

type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

type Repository interface {
	CreateComment(ctx context.Context, c *model.Comment) error
	GetComment(ctx context.Context, id int64) (*model.Comment, error)
	ListComments(ctx context.Context, videoID int64, limit, offset int) ([]model.Comment, int, error)
	ListReplies(ctx context.Context, parentID int64) ([]model.Comment, error)
	AdminListComments(ctx context.Context, search string, limit, offset int) ([]model.Comment, int, error)
	UpdateComment(ctx context.Context, id int64, text string) error
	DeleteComment(ctx context.Context, id int64) error
	GetReaction(ctx context.Context, videoID, userID int64) (int16, error)
	SetReaction(ctx context.Context, videoID, userID int64, value int16) error
	RemoveReaction(ctx context.Context, videoID, userID int64) error
	CountReactions(ctx context.Context, videoID int64) (int64, int64, error)
	Subscribe(ctx context.Context, subscriberID int64, subscriberName string, channelID int64, channelName string) error
	Unsubscribe(ctx context.Context, subscriberID, channelID int64) error
	ListSubscriptions(ctx context.Context, subscriberID int64) ([]model.Subscription, error)
	ListSubscribers(ctx context.Context, channelID int64) ([]model.Subscriber, error)
	SetSubscriberName(ctx context.Context, subscriberID int64, name string) error
	CountSubscribers(ctx context.Context, channelID int64) (int64, error)
	DeleteVideoData(ctx context.Context, videoID int64) error
	DeleteUserData(ctx context.Context, userID int64) ([]int64, error)
	Stats(ctx context.Context) (*model.Stats, error)
}

type Cache interface {
	GetCounts(ctx context.Context, videoID int64) (int64, int64, bool, error)
	SetCounts(ctx context.Context, videoID, likes, dislikes int64) error
	Invalidate(ctx context.Context, videoID int64) error
}

type VideoClient interface {
	Exists(ctx context.Context, videoID int64) (bool, error)
}

type AuthClient interface {
	GetUsername(ctx context.Context, userID int64) (string, bool, error)
}

type Service struct {
	repo   Repository
	cache  Cache
	videos VideoClient
	users  AuthClient
}

func New(repo Repository, cache Cache, videos VideoClient, users AuthClient) *Service {
	return &Service{repo: repo, cache: cache, videos: videos, users: users}
}

// Actor — кто выполняет действие. Администратор может менять любые комментарии.
type Actor struct {
	UserID  int64
	IsAdmin bool
}

// ---------- Комментарии ----------

// AddComment добавляет комментарий или ответ (если parentID не nil).
// Ответы одноуровневые, как на YouTube: ответ на ответ привязывается к корневому комментарию.
func (s *Service) AddComment(ctx context.Context, videoID, userID int64, username, text string, parentID *int64) (*model.Comment, error) {
	text, err := validateText(text)
	if err != nil {
		return nil, err
	}

	if parentID != nil {
		parent, err := s.repo.GetComment(ctx, *parentID)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCommentNotFound
		}
		if err != nil {
			return nil, err
		}
		if parent.VideoID != videoID {
			return nil, &ValidationError{"parent comment belongs to another video"}
		}
		if parent.ParentID != nil {
			parentID = parent.ParentID
		}
	} else if err := s.ensureVideoExists(ctx, videoID); err != nil {
		return nil, err
	}

	c := &model.Comment{VideoID: videoID, UserID: userID, Username: username, Text: text, ParentID: parentID}
	if err := s.repo.CreateComment(ctx, c); err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}
	return c, nil
}

func (s *Service) ListComments(ctx context.Context, videoID int64, page, limit int) (*model.CommentPage, error) {
	items, total, err := s.repo.ListComments(ctx, videoID, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	return &model.CommentPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) ListReplies(ctx context.Context, commentID int64) ([]model.Comment, error) {
	return s.repo.ListReplies(ctx, commentID)
}

func (s *Service) EditComment(ctx context.Context, actor Actor, commentID int64, text string) (*model.Comment, error) {
	if _, err := s.editableComment(ctx, actor, commentID); err != nil {
		return nil, err
	}
	text, err := validateText(text)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateComment(ctx, commentID, text); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCommentNotFound
		}
		return nil, err
	}
	return s.repo.GetComment(ctx, commentID)
}

func (s *Service) DeleteComment(ctx context.Context, actor Actor, commentID int64) error {
	if _, err := s.editableComment(ctx, actor, commentID); err != nil {
		return err
	}
	if err := s.repo.DeleteComment(ctx, commentID); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	return nil
}

func (s *Service) AdminComments(ctx context.Context, search string, page, limit int) (*model.CommentPage, error) {
	items, total, err := s.repo.AdminListComments(ctx, strings.TrimSpace(search), limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	return &model.CommentPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) editableComment(ctx context.Context, actor Actor, commentID int64) (*model.Comment, error) {
	c, err := s.repo.GetComment(ctx, commentID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrCommentNotFound
	}
	if err != nil {
		return nil, err
	}
	if !actor.IsAdmin && c.UserID != actor.UserID {
		return nil, ErrForbidden
	}
	return c, nil
}

func validateText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n == 0 || n > 1000 {
		return "", &ValidationError{"comment must be 1-1000 characters long"}
	}
	return text, nil
}

// ---------- Реакции ----------

// React ставит лайк или дизлайк. Повторное нажатие той же кнопки снимает реакцию,
// как это сделано на YouTube.
func (s *Service) React(ctx context.Context, videoID, userID int64, value int16) (*model.ReactionStats, error) {
	if err := s.ensureVideoExists(ctx, videoID); err != nil {
		return nil, err
	}

	current, err := s.repo.GetReaction(ctx, videoID, userID)
	if err != nil {
		return nil, err
	}
	if current == value {
		err = s.repo.RemoveReaction(ctx, videoID, userID)
	} else {
		err = s.repo.SetReaction(ctx, videoID, userID, value)
	}
	if err != nil {
		return nil, err
	}

	if err := s.cache.Invalidate(ctx, videoID); err != nil {
		slog.Warn("cache invalidate failed", "video_id", videoID, "err", err)
	}
	return s.Stats(ctx, videoID, userID)
}

// Stats возвращает количество лайков/дизлайков. Если userID > 0 — ещё и реакцию этого пользователя.
func (s *Service) Stats(ctx context.Context, videoID, userID int64) (*model.ReactionStats, error) {
	likes, dislikes, found, err := s.cache.GetCounts(ctx, videoID)
	if err != nil {
		slog.Warn("cache read failed", "video_id", videoID, "err", err)
	}
	if !found {
		likes, dislikes, err = s.repo.CountReactions(ctx, videoID)
		if err != nil {
			return nil, err
		}
		if err := s.cache.SetCounts(ctx, videoID, likes, dislikes); err != nil {
			slog.Warn("cache write failed", "video_id", videoID, "err", err)
		}
	}

	stats := &model.ReactionStats{VideoID: videoID, Likes: likes, Dislikes: dislikes}
	if userID > 0 {
		my, err := s.repo.GetReaction(ctx, videoID, userID)
		if err != nil {
			return nil, err
		}
		switch my {
		case model.ReactionLike:
			stats.MyReaction = "like"
		case model.ReactionDislike:
			stats.MyReaction = "dislike"
		}
	}
	return stats, nil
}

// ---------- Подписки ----------

// Subscribe подписывает на канал. Канал — это пользователь; видео у него может ещё не быть.
func (s *Service) Subscribe(ctx context.Context, userID int64, username string, channelID int64) error {
	if userID == channelID {
		return &ValidationError{"you cannot subscribe to yourself"}
	}
	name, found, err := s.users.GetUsername(ctx, channelID)
	if err != nil {
		return fmt.Errorf("check channel: %w", err)
	}
	if !found {
		return ErrChannelNotFound
	}
	return s.repo.Subscribe(ctx, userID, username, channelID, name)
}

func (s *Service) Unsubscribe(ctx context.Context, userID, channelID int64) error {
	err := s.repo.Unsubscribe(ctx, userID, channelID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotSubscribed
	}
	return err
}

func (s *Service) Subscriptions(ctx context.Context, userID int64) ([]model.Subscription, error) {
	return s.repo.ListSubscriptions(ctx, userID)
}

// Subscribers — список подписчиков канала. Подписки, оформленные до появления колонки
// subscriber_name, хранят пустое имя: его спрашиваем у auth-service и сохраняем,
// чтобы в следующий раз не ходить за ним снова.
func (s *Service) Subscribers(ctx context.Context, channelID int64) ([]model.Subscriber, error) {
	subs, err := s.repo.ListSubscribers(ctx, channelID)
	if err != nil {
		return nil, err
	}
	for i := range subs {
		if subs[i].Username != "" {
			continue
		}
		name, found, err := s.users.GetUsername(ctx, subs[i].UserID)
		if err != nil || !found {
			continue
		}
		subs[i].Username = name
		if err := s.repo.SetSubscriberName(ctx, subs[i].UserID, name); err != nil {
			slog.Warn("failed to save subscriber name", "user_id", subs[i].UserID, "err", err)
		}
	}
	return subs, nil
}

func (s *Service) SubscribersCount(ctx context.Context, channelID int64) (int64, error) {
	return s.repo.CountSubscribers(ctx, channelID)
}

// ---------- Удаление и статистика ----------

// DeleteVideoData вызывается video-service при удалении видео.
func (s *Service) DeleteVideoData(ctx context.Context, videoID int64) error {
	if err := s.repo.DeleteVideoData(ctx, videoID); err != nil {
		return err
	}
	_ = s.cache.Invalidate(ctx, videoID)
	return nil
}

// DeleteUserData вызывается auth-service при удалении аккаунта.
func (s *Service) DeleteUserData(ctx context.Context, userID int64) error {
	videoIDs, err := s.repo.DeleteUserData(ctx, userID)
	if err != nil {
		return err
	}
	for _, id := range videoIDs {
		_ = s.cache.Invalidate(ctx, id)
	}
	return nil
}

func (s *Service) AdminStats(ctx context.Context) (*model.Stats, error) {
	return s.repo.Stats(ctx)
}

func (s *Service) ensureVideoExists(ctx context.Context, videoID int64) error {
	exists, err := s.videos.Exists(ctx, videoID)
	if err != nil {
		return fmt.Errorf("check video: %w", err)
	}
	if !exists {
		return ErrVideoNotFound
	}
	return nil
}
