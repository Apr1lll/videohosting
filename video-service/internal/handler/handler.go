// Package handler — HTTP-слой video-service.
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"videohosting/video-service/internal/httpx"
	"videohosting/video-service/internal/service"
)

type Handler struct {
	svc            *service.VideoService
	maxUploadBytes int64
}

func New(svc *service.VideoService, maxUploadBytes int64) *Handler {
	return &Handler{svc: svc, maxUploadBytes: maxUploadBytes}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /videos", h.upload)
	mux.HandleFunc("GET /videos", h.list)
	mux.HandleFunc("GET /videos/popular", h.popular)
	mux.HandleFunc("GET /videos/{id}", h.get)
	mux.HandleFunc("GET /videos/{id}/stream", h.stream)
	mux.HandleFunc("DELETE /videos/{id}", h.delete)
	return httpx.Recover(httpx.Logging(mux))
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// upload принимает multipart/form-data с полями title, description и файлом file.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)
	// Первые 32 МБ держим в памяти, остальное Go сам сбросит во временный файл.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "file is too large")
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "field 'file' is required")
		return
	}
	defer file.Close()

	video, err := h.svc.Upload(r.Context(), service.UploadInput{
		AuthorID:    userID,
		AuthorName:  r.Header.Get("X-Username"),
		Title:       r.FormValue("title"),
		Description: r.FormValue("description"),
		FileName:    header.Filename,
		File:        file,
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, video)
}

// list: GET /videos?search=котики&author_id=1&page=1&limit=20
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.Pagination(r)

	var authorID int64
	if raw := r.URL.Query().Get("author_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid author_id")
			return
		}
		authorID = id
	}

	result, err := h.svc.List(r.Context(), r.URL.Query().Get("search"), authorID, page, limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) popular(w http.ResponseWriter, r *http.Request) {
	videos, err := h.svc.Popular(r.Context())
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": videos})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	video, err := h.svc.Get(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, video)
}

// stream отдаёт файл видео. http.ServeContent сам поддерживает заголовок Range,
// поэтому в браузерном плеере работает перемотка.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	video, file, err := h.svc.OpenStream(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	defer file.Close()

	if isFirstChunk(r) {
		h.svc.RegisterView(r.Context(), id)
	}

	w.Header().Set("Content-Type", video.ContentType)
	http.ServeContent(w, r, video.FileName, video.CreatedAt, file)
}

// isFirstChunk — просмотр засчитываем только при запросе начала файла,
// а не на каждый Range-запрос при перемотке.
func isFirstChunk(r *http.Request) bool {
	rng := r.Header.Get("Range")
	return rng == "" || strings.HasPrefix(rng, "bytes=0-")
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Delete(r.Context(), id, userID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	var vErr *service.ValidationError
	switch {
	case errors.As(err, &vErr):
		httpx.WriteError(w, http.StatusBadRequest, vErr.Msg)
	case errors.Is(err, service.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, err.Error())
	default:
		slog.Error("internal error", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
