package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// RequestID присваивает каждому запросу уникальный ID и передаёт его в сервисы,
// чтобы по логам можно было отследить путь запроса через всю систему.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
			r.Header.Set("X-Request-ID", id)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap нужен, чтобы http.ResponseController мог добраться до исходного ResponseWriter
// (например, для Flush при проксировании и стриминге видео).
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", r.Header.Get("X-Request-ID"),
			"ip", clientIP(r),
		)
	})
}

// CORS разрешает обращаться к API из браузерного фронтенда.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Content-Range, Accept-Ranges")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimiter — простой ограничитель «N запросов в минуту с одного IP»
// по алгоритму фиксированного окна.
type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clients map[string]*clientWindow
}

type clientWindow struct {
	count int
	start time.Time
}

func NewRateLimiter(limitPerMinute int) *RateLimiter {
	rl := &RateLimiter{
		limit:   limitPerMinute,
		window:  time.Minute,
		clients: make(map[string]*clientWindow),
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cw, ok := rl.clients[ip]
	if !ok || now.Sub(cw.start) >= rl.window {
		rl.clients[ip] = &clientWindow{count: 1, start: now}
		return true
	}
	if cw.count >= rl.limit {
		return false
	}
	cw.count++
	return true
}

// cleanup раз в минуту удаляет старые записи, чтобы map не рос бесконечно.
func (rl *RateLimiter) cleanup() {
	for range time.Tick(rl.window) {
		rl.mu.Lock()
		for ip, cw := range rl.clients {
			if time.Since(cw.start) >= rl.window {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware ограничивает только изменяющие запросы (POST, PATCH, PUT, DELETE):
// перебор паролей, спам комментариями, массовые загрузки. Чтение (страницы, превью,
// аватарки, видео) не ограничиваем — одна страница сайта тянет десятки картинок.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if !rl.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "too many requests, try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
