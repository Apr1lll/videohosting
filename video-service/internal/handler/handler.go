// Package handler — HTTP-слой video-service.
package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
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
	mux.HandleFunc("PATCH /videos/{id}", h.update)
	mux.HandleFunc("DELETE /videos/{id}", h.delete)
	mux.HandleFunc("GET /videos/{id}/stream", h.stream)
	mux.HandleFunc("POST /videos/{id}/view", h.view)
	mux.HandleFunc("GET /videos/{id}/thumbnail", h.thumbnail)
	mux.HandleFunc("PUT /videos/{id}/thumbnail", h.setThumbnail)
	mux.HandleFunc("GET /videos/{id}/recommendations", h.recommendations)

	mux.HandleFunc("GET /admin/videos/stats", h.adminStats)

	// Внутренний эндпоинт: вызывает auth-service при удалении пользователя.
	mux.HandleFunc("DELETE /internal/users/{id}/videos", h.deleteUserVideos)
	return httpx.Recover(httpx.Logging(mux))
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func actor(r *http.Request) (service.Actor, bool) {
	id, ok := httpx.UserID(r)
	return service.Actor{UserID: id, IsAdmin: httpx.IsAdmin(r)}, ok
}

// upload принимает multipart/form-data: title, description, duration, thumbnail (картинка) и file.
//
// Тело читается потоково через MultipartReader: видео пишется сразу в хранилище,
// без копии во временную папку, как было бы с ParseMultipartForm. Это вдвое сокращает
// запись на диск и заметно ускоряет загрузку больших файлов.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}

	in := service.CreateInput{AuthorID: userID, AuthorName: r.Header.Get("X-Username")}
	fail := func(err error) {
		h.svc.DiscardVideoFile(in.File)
		h.handleError(w, err)
	}

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(err)
			return
		}

		switch part.FormName() {
		case "title":
			in.Title, err = readField(part, 1<<10)
		case "description":
			in.Description, err = readField(part, 32<<10)
		case "duration":
			var raw string
			if raw, err = readField(part, 32); err == nil {
				f, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
				in.DurationSeconds = int(f + 0.5)
			}
		case "thumbnail":
			in.Thumbnail, err = io.ReadAll(io.LimitReader(part, 5<<20+1))
		case "file":
			if in.File != nil {
				err = &service.ValidationError{Msg: "only one file can be uploaded"}
				break
			}
			in.File, err = h.svc.SaveVideoFile(part, part.FileName())
		}
		part.Close()
		if err != nil {
			fail(err)
			return
		}
	}

	video, err := h.svc.Create(r.Context(), in)
	if err != nil {
		h.handleError(w, err) // Create сам удаляет файл при ошибке
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, video)
}

func readField(r io.Reader, limit int64) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		return "", &service.ValidationError{Msg: "form field is too long"}
	}
	return string(data), nil
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

func (h *Handler) recommendations(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 || limit > 50 {
		limit = 20
	}
	videos, err := h.svc.Recommendations(r.Context(), id, limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": videos})
}

type updateRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// update меняет название и описание. Доступно автору видео и администратору.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	video, err := h.svc.UpdateInfo(r.Context(), a, id, req.Title, req.Description)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, video)
}

// setThumbnail заменяет превью: multipart-поле "thumbnail" с картинкой.
func (h *Handler) setThumbnail(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	file, _, err := r.FormFile("thumbnail")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "field 'thumbnail' is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		h.handleError(w, err)
		return
	}
	video, err := h.svc.SetThumbnail(r.Context(), a, id, data)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, video)
}

func (h *Handler) thumbnail(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	video, file, err := h.svc.OpenThumbnail(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	defer file.Close()
	// Ссылка содержит ?v=<время изменения>, поэтому картинку можно кешировать надолго.
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	http.ServeContent(w, r, video.ThumbnailFile, video.UpdatedAt, file)
}

// stream отдаёт файл видео. http.ServeContent сам поддерживает заголовок Range,
// поэтому в плеере работает перемотка.
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

	// Видеофайл после загрузки не меняется — браузер может держать его в кеше
	// и при повторном просмотре не скачивать заново.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Type", video.ContentType)
	http.ServeContent(w, r, video.FileName, video.CreatedAt, file)
}

// view засчитывает просмотр. Фронтенд вызывает его, когда зритель посмотрел первые секунды.
// Каждый зритель учитывается один раз: вошедший — по ID пользователя,
// гость — по хешу IP-адреса и браузера (User-Agent).
func (h *Handler) view(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := h.svc.RegisterView(r.Context(), id, viewerKey(r))
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func viewerKey(r *http.Request) string {
	if userID, ok := httpx.UserID(r); ok {
		return "u:" + strconv.FormatInt(userID, 10)
	}
	// X-Real-IP выставляет gateway (значение от клиента он отбрасывает).
	sum := sha256.Sum256([]byte(r.Header.Get("X-Real-IP") + "|" + r.Header.Get("User-Agent")))
	return "a:" + hex.EncodeToString(sum[:16])
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Delete(r.Context(), a, id); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) adminStats(w http.ResponseWriter, r *http.Request) {
	if !httpx.IsAdmin(r) {
		httpx.WriteError(w, http.StatusForbidden, "admin rights required")
		return
	}
	stats, err := h.svc.AdminStats(r.Context())
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
}

func (h *Handler) deleteUserVideos(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := h.svc.DeleteUserVideos(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"deleted": n})
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	var vErr *service.ValidationError
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &vErr):
		httpx.WriteError(w, http.StatusBadRequest, vErr.Msg)
	case errors.As(err, &tooBig):
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "file is too large")
	case errors.Is(err, service.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, err.Error())
	default:
		slog.Error("internal error", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
