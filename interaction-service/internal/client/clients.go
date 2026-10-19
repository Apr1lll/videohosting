// Package client — HTTP-клиенты для обращения к video-service и auth-service.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 5 * time.Second}

// VideoClient проверяет существование видео в video-service.
type VideoClient struct {
	baseURL string
}

func NewVideoClient(baseURL string) *VideoClient {
	return &VideoClient{baseURL: baseURL}
}

func (c *VideoClient) Exists(ctx context.Context, videoID int64) (bool, error) {
	status, err := get(ctx, fmt.Sprintf("%s/videos/%d", c.baseURL, videoID), nil)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("video-service returned %d", status)
	}
}

// AuthClient получает данные пользователей из auth-service.
type AuthClient struct {
	baseURL string
}

func NewAuthClient(baseURL string) *AuthClient {
	return &AuthClient{baseURL: baseURL}
}

// GetUsername возвращает имя пользователя по ID. found=false — пользователя нет.
func (c *AuthClient) GetUsername(ctx context.Context, userID int64) (username string, found bool, err error) {
	var user struct {
		Username string `json:"username"`
	}
	status, err := get(ctx, fmt.Sprintf("%s/users/%d", c.baseURL, userID), &user)
	if err != nil {
		return "", false, err
	}
	switch status {
	case http.StatusOK:
		return user.Username, true, nil
	case http.StatusNotFound:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("auth-service returned %d", status)
	}
}

// get выполняет GET-запрос и при статусе 200 разбирает JSON в dst (если dst != nil).
func get(ctx context.Context, url string, dst any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK && dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return 0, fmt.Errorf("decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
}
