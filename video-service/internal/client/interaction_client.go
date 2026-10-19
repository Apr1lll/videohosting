// Package client — HTTP-клиенты для обращения к другим микросервисам.
package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// InteractionClient обращается к interaction-service.
type InteractionClient struct {
	baseURL string
	http    *http.Client
}

func NewInteractionClient(baseURL string) *InteractionClient {
	return &InteractionClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

// DeleteVideoData просит interaction-service удалить комментарии и реакции удалённого видео.
func (c *InteractionClient) DeleteVideoData(ctx context.Context, videoID int64) error {
	url := fmt.Sprintf("%s/internal/videos/%d", c.baseURL, videoID)
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
		return fmt.Errorf("interaction-service returned %d", resp.StatusCode)
	}
	return nil
}
