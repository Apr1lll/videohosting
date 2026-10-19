// Package client — обращения auth-service к другим сервисам.
package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// UserDataCleaner удаляет данные пользователя в других сервисах при удалении аккаунта.
type UserDataCleaner struct {
	videoURL       string
	interactionURL string
	http           *http.Client
}

func NewUserDataCleaner(videoURL, interactionURL string) *UserDataCleaner {
	return &UserDataCleaner{
		videoURL:       videoURL,
		interactionURL: interactionURL,
		http:           &http.Client{Timeout: 60 * time.Second}, // у пользователя может быть много видео
	}
}

// DeleteUserData удаляет видео пользователя (video-service), а также его комментарии,
// реакции и подписки (interaction-service).
func (c *UserDataCleaner) DeleteUserData(ctx context.Context, userID int64) error {
	if err := c.delete(ctx, fmt.Sprintf("%s/internal/users/%d/videos", c.videoURL, userID)); err != nil {
		return fmt.Errorf("video-service: %w", err)
	}
	if err := c.delete(ctx, fmt.Sprintf("%s/internal/users/%d", c.interactionURL, userID)); err != nil {
		return fmt.Errorf("interaction-service: %w", err)
	}
	return nil
}

func (c *UserDataCleaner) delete(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
