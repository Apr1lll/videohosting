// Package gateway — единая точка входа: маршрутизация запросов по сервисам и проверка JWT.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"videohosting/api-gateway/internal/auth"
)

type backend struct {
	name  string
	url   *url.URL
	proxy *httputil.ReverseProxy
}

type Gateway struct {
	verifier    *auth.Verifier
	auth        *backend
	video       *backend
	interaction *backend
	healthHTTP  *http.Client
	static      http.Handler // раздача фронтенда; nil — фронтенд отключён
}

// New создаёт gateway. webDir — папка с фронтендом (index.html, app.js, styles.css);
// пустая строка отключает раздачу статики.
func New(verifier *auth.Verifier, authURL, videoURL, interactionURL, webDir string) (*Gateway, error) {
	a, err := newBackend("auth-service", authURL)
	if err != nil {
		return nil, err
	}
	v, err := newBackend("video-service", videoURL)
	if err != nil {
		return nil, err
	}
	i, err := newBackend("interaction-service", interactionURL)
	if err != nil {
		return nil, err
	}
	g := &Gateway{
		verifier:    verifier,
		auth:        a,
		video:       v,
		interaction: i,
		healthHTTP:  &http.Client{Timeout: 2 * time.Second},
	}
	if webDir != "" {
		g.static = noCache(http.FileServer(http.Dir(webDir)))
	}
	return g, nil
}

// noCache заставляет браузер проверять обновления фронтенда при каждом заходе.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func newBackend(name, rawURL string) (*backend, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("backend unavailable", "backend", name, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusBadGateway, name+" is unavailable")
	}
	return &backend{name: name, url: u, proxy: proxy}, nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		g.health(w, r)
		return
	}
	// Внутренние эндпоинты сервисов снаружи недоступны.
	if strings.HasPrefix(r.URL.Path, "/internal/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Клиент не должен иметь возможность подставить чужой ID сам —
	// эти заголовки выставляет только gateway после проверки токена.
	r.Header.Del("X-User-ID")
	r.Header.Del("X-Username")
	r.Header.Del("X-User-Role")

	isAdmin := false
	if header := r.Header.Get("Authorization"); header != "" {
		id, err := g.verifier.Verify(header)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		r.Header.Set("X-User-ID", strconv.FormatInt(id.UserID, 10))
		r.Header.Set("X-Username", id.Username)
		r.Header.Set("X-User-Role", id.Role)
		isAdmin = id.Role == "admin"
	}

	// Админ-панель: пускаем только администраторов. Сервисы проверяют роль ещё раз.
	if strings.HasPrefix(r.URL.Path, "/admin/") && !isAdmin {
		writeError(w, http.StatusForbidden, "admin rights required")
		return
	}

	b := g.route(r.URL.Path)
	if b == nil {
		// Всё, что не относится к API, — это файлы фронтенда.
		if g.static != nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			g.static.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	b.proxy.ServeHTTP(w, r)
}

// route выбирает сервис по пути запроса.
func (g *Gateway) route(path string) *backend {
	switch {
	case strings.HasPrefix(path, "/auth/"):
		return g.auth
	case path == "/users/me/subscriptions", path == "/users/me/subscribers":
		return g.interaction
	case strings.HasPrefix(path, "/admin/users"):
		return g.auth
	case strings.HasPrefix(path, "/admin/videos"):
		return g.video
	case strings.HasPrefix(path, "/admin/comments"), strings.HasPrefix(path, "/admin/interactions"):
		return g.interaction
	case strings.HasPrefix(path, "/users/"):
		return g.auth
	case strings.HasPrefix(path, "/comments/"), strings.HasPrefix(path, "/channels/"):
		return g.interaction
	case isInteractionVideoPath(path):
		return g.interaction
	case path == "/videos" || strings.HasPrefix(path, "/videos/"):
		return g.video
	}
	return nil
}

// isInteractionVideoPath — пути вида /videos/{id}/comments|like|dislike|reactions
// относятся к interaction-service, остальные /videos/... — к video-service.
func isInteractionVideoPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "videos" {
		return false
	}
	switch parts[2] {
	case "comments", "like", "dislike", "reactions":
		return true
	}
	return false
}

// health опрашивает все сервисы параллельно и возвращает сводный статус.
func (g *Gateway) health(w http.ResponseWriter, r *http.Request) {
	backends := []*backend{g.auth, g.video, g.interaction}
	statuses := make(map[string]string, len(backends))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, b := range backends {
		wg.Add(1)
		go func(b *backend) {
			defer wg.Done()
			status := "ok"
			if err := g.ping(r.Context(), b); err != nil {
				status = "down"
			}
			mu.Lock()
			statuses[b.name] = status
			mu.Unlock()
		}(b)
	}
	wg.Wait()

	code := http.StatusOK
	overall := "ok"
	for _, s := range statuses {
		if s != "ok" {
			code = http.StatusServiceUnavailable
			overall = "degraded"
		}
	}
	writeJSON(w, code, map[string]any{"status": overall, "services": statuses})
}

func (g *Gateway) ping(ctx context.Context, b *backend) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url.String()+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := g.healthHTTP.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d", b.name, resp.StatusCode)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
