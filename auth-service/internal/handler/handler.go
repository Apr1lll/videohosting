// Package handler — HTTP-слой auth-service.
package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"videohosting/auth-service/internal/httpx"
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
	mux.HandleFunc("GET /users/{id}", h.getUser)
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
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
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
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
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

// handleError переводит ошибки бизнес-логики в HTTP-статусы.
func (h *Handler) handleError(w http.ResponseWriter, err error) {
	var vErr *service.ValidationError
	switch {
	case errors.As(err, &vErr):
		httpx.WriteError(w, http.StatusBadRequest, vErr.Msg)
	case errors.Is(err, service.ErrUserExists):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrInvalidCredentials):
		httpx.WriteError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, service.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	default:
		slog.Error("internal error", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
