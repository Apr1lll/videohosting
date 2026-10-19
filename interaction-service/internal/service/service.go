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
	ErrForbidden       = errors.New("you can delete only your own comments")
)

type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

type Repository interface {
	CreateComment(ctx context.Context, c *model.Comment) error
	GetComment(ctx context.Context, id int64) (*model.Comment, error)
	ListComments(ctx context.Context, videoID int64, limit, offset int) ([]model.Comment, int, error)
	DeleteComment(ctx context.Context, id int64) error
	GetReaction(ctx context.Context, videoID, userID int64) (int16, error)
	SetReaction(ctx context.Context, videoID, userID int64, value int16) error
	RemoveReaction(ctx context.Context, videoID, userID int64) error
	CountReactions(ctx context.Context, videoID int64) (int64, int64, error)
	Subscribe(ctx context.Context, subscriberID, channelID int64, channelName string) error
	Unsubscribe(ctx context.Context, subscriberID, channelID int64) error
	ListSubscriptions(ctx context.Context, subscriberID int64) ([]model.Subscription, error)
	CountSubscribers(ctx context.Context, channelID int64) (int64, error)
	DeleteVideoData(ctx context.Context, videoID int64) error
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

// ---------- Комментарии ----------

func (s *Service) AddComment(ctx context.Context, videoID, userID int64, username, text string) (*model.Comment, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n == 0 || n > 1000 {
		return nil, &ValidationError{"comment must be 1-1000 characters long"}
	}
	if err := s.ensureVideoExists(ctx, videoID); err != nil {
		return nil, err
	}

	c := &model.Comment{VideoID: videoID, UserID: userID, Username: username, Text: text}
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

func (s *Service) DeleteComment(ctx context.Context, commentID, userID int64) error {
	c, err := s.repo.GetComment(ctx, commentID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrCommentNotFound
	}
	if err != nil {
		return err
	}
	if c.UserID != userID {
		return ErrForbidden
	}
	if err := s.repo.DeleteComment(ctx, commentID); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	return nil
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

func (s *Service) Subscribe(ctx context.Context, userID, channelID int64) error {
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
	return s.repo.Subscribe(ctx, userID, channelID, name)
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

func (s *Service) SubscribersCount(ctx context.Context, channelID int64) (int64, error) {
	return s.repo.CountSubscribers(ctx, channelID)
}

// DeleteVideoData вызывается video-service при удалении видео.
func (s *Service) DeleteVideoData(ctx context.Context, videoID int64) error {
	if err := s.repo.DeleteVideoData(ctx, videoID); err != nil {
		return err
	}
	_ = s.cache.Invalidate(ctx, videoID)
	return nil
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
