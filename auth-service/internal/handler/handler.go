// Package handler — HTTP-слой auth-service.
package handler

import (
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"videohosting/auth-service/internal/httpx"
	"videohosting/auth-service/internal/model"
	"videohosting/auth-service/internal/service"
)

type Handler struct {
	svc *service.AuthService
}

func New(svc *service.AuthService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /auth/register", h.register)
	mux.HandleFunc("POST /auth/login", h.login)

	mux.HandleFunc("GET /users/me", h.me)
	mux.HandleFunc("PATCH /users/me", h.updateMe)
	mux.HandleFunc("PUT /users/me/avatar", h.uploadAvatar)
	mux.HandleFunc("DELETE /users/me/avatar", h.deleteMyAvatar)
	mux.HandleFunc("GET /users/{id}", h.getUser)
	mux.HandleFunc("GET /users/{id}/avatar", h.avatar)

	// Админ-панель. Gateway пропускает /admin/* только администраторам,
	// а здесь роль проверяется ещё раз (защита в глубину).
	mux.HandleFunc("GET /admin/users", h.adminOnly(h.adminListUsers))
	mux.HandleFunc("PATCH /admin/users/{id}", h.adminOnly(h.adminUpdateUser))
	mux.HandleFunc("DELETE /admin/users/{id}", h.adminOnly(h.adminDeleteUser))
	mux.HandleFunc("DELETE /admin/users/{id}/avatar", h.adminOnly(h.adminDeleteAvatar))

	return httpx.Recover(httpx.Logging(mux))
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.svc.Register(r.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, user)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	user, err := h.svc.GetUser(r.Context(), userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, user)
}

type updateMeRequest struct {
	Bio string `json:"bio"`
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	var req updateMeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.svc.UpdateBio(r.Context(), userID, req.Bio)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, user)
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.svc.GetUser(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, user.Public())
}

// ---------- Аватарки ----------

// uploadAvatar принимает картинку в поле "avatar" (multipart/form-data).
func (h *Handler) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	file, _, err := r.FormFile("avatar")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "field 'avatar' is required")
		return
	}
	defer file.Close()

	user, err := h.svc.SetAvatar(r.Context(), userID, file)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, user)
}

func (h *Handler) deleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	user, err := h.svc.DeleteAvatar(r.Context(), userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, user)
}

// avatar отдаёт картинку пользователя, а если её нет — рисует SVG с первой буквой имени.
// Так фронтенд всегда может просто вставить <img src="/users/{id}/avatar">.
func (h *Handler) avatar(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, file, err := h.svc.OpenAvatar(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}

	// Если в ссылке есть ?v=..., содержимое по ней никогда не меняется — можно кешировать надолго.
	if r.URL.Query().Get("v") != "" {
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	} else {
		// Без версии браузер каждый раз сверяется с сервером (ответ 304 — почти бесплатно),
		// поэтому новая аватарка сразу видна под видео и в комментариях.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Last-Modified", user.UpdatedAt.UTC().Format(http.TimeFormat))
	}

	if file == nil {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(defaultAvatar(user)))
		return
	}
	defer file.Close()
	http.ServeContent(w, r, user.AvatarFile, user.UpdatedAt, file)
}

var avatarColors = []string{"#16213A", "#C2185B", "#0F766E", "#9A6212", "#5B21B6", "#334155", "#1D4ED8", "#7C2D12"}

func defaultAvatar(u *model.User) string {
	letter := "?"
	if r, _ := utf8.DecodeRuneInString(u.Username); r != utf8.RuneError {
		letter = strings.ToUpper(string(r))
	}
	color := avatarColors[(u.ID*7+24)%int64(len(avatarColors))]
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">`+
		`<rect width="64" height="64" fill="%s"/>`+
		`<text x="32" y="33" fill="#fff" font-family="Arial, sans-serif" font-size="30" font-weight="700" `+
		`text-anchor="middle" dominant-baseline="central">%s</text></svg>`, color, html.EscapeString(letter))
}

// ---------- Админ-панель ----------

func (h *Handler) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := httpx.UserID(r); !ok || !httpx.IsAdmin(r) {
			httpx.WriteError(w, http.StatusForbidden, "admin rights required")
			return
		}
		next(w, r)
	}
}

func (h *Handler) adminListUsers(w http.ResponseWriter, r *http.Request) {
	page, limit := httpx.Pagination(r)
	res, err := h.svc.AdminListUsers(r.Context(), r.URL.Query().Get("search"), page, limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	actorID, _ := httpx.UserID(r)
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var patch model.AdminPatch
	if err := httpx.DecodeJSON(w, r, &patch); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.svc.AdminUpdateUser(r.Context(), actorID, id, patch)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, user)
}

func (h *Handler) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	actorID, _ := httpx.UserID(r)
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.AdminDeleteUser(r.Context(), actorID, id); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) adminDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.svc.DeleteAvatar(r.Context(), id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, user)
}

// ---------- Вспомогательное ----------

func (h *Handler) requireUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
	}
	return userID, ok
}

// handleError переводит ошибки бизнес-логики в HTTP-статусы.
func (h *Handler) handleError(w http.ResponseWriter, err error) {
	var vErr *service.ValidationError
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &vErr):
		httpx.WriteError(w, http.StatusBadRequest, vErr.Msg)
	case errors.As(err, &tooBig):
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "file is too large")
	case errors.Is(err, service.ErrUserExists):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrInvalidCredentials):
		httpx.WriteError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, service.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrSelfAction):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Error("internal error", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
