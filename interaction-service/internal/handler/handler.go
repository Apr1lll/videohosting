// Package handler — HTTP-слой interaction-service.
package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"videohosting/interaction-service/internal/httpx"
	"videohosting/interaction-service/internal/model"
	"videohosting/interaction-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)

	mux.HandleFunc("POST /videos/{id}/comments", h.addComment)
	mux.HandleFunc("GET /videos/{id}/comments", h.listComments)
	mux.HandleFunc("DELETE /comments/{id}", h.deleteComment)

	mux.HandleFunc("POST /videos/{id}/like", h.react(model.ReactionLike))
	mux.HandleFunc("POST /videos/{id}/dislike", h.react(model.ReactionDislike))
	mux.HandleFunc("GET /videos/{id}/reactions", h.reactions)

	mux.HandleFunc("POST /channels/{id}/subscribe", h.subscribe)
	mux.HandleFunc("DELETE /channels/{id}/subscribe", h.unsubscribe)
	mux.HandleFunc("GET /channels/{id}/subscribers", h.subscribers)
	mux.HandleFunc("GET /users/me/subscriptions", h.mySubscriptions)

	// Внутренний эндпоинт: вызывается только video-service, наружу gateway его не пропускает.
	mux.HandleFunc("DELETE /internal/videos/{id}", h.deleteVideoData)

	return httpx.Recover(httpx.Logging(mux))
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type commentRequest struct {
	Text string `json:"text"`
}

func (h *Handler) addComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	videoID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req commentRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	comment, err := h.svc.AddComment(r.Context(), videoID, userID, r.Header.Get("X-Username"), req.Text)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, comment)
}

func (h *Handler) listComments(w http.ResponseWriter, r *http.Request) {
	videoID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, limit := httpx.Pagination(r)
	result, err := h.svc.ListComments(r.Context(), videoID, page, limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) deleteComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	commentID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.DeleteComment(r.Context(), commentID, userID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// react возвращает обработчик для лайка или дизлайка — логика у них одинаковая.
func (h *Handler) react(value int16) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := httpx.UserID(r)
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
			return
		}
		videoID, err := httpx.PathID(r, "id")
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		stats, err := h.svc.React(r.Context(), videoID, userID, value)
		if err != nil {
			h.handleError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, stats)
	}
}

func (h *Handler) reactions(w http.ResponseWriter, r *http.Request) {
	videoID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID, _ := httpx.UserID(r) // авторизация не обязательна; если есть — покажем свою реакцию
	stats, err := h.svc.Stats(r.Context(), videoID, userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	channelID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Subscribe(r.Context(), userID, channelID); err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"subscribed": true, "channel_id": channelID})
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	channelID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Unsubscribe(r.Context(), userID, channelID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) subscribers(w http.ResponseWriter, r *http.Request) {
	channelID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := h.svc.SubscribersCount(r.Context(), channelID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"channel_id": channelID, "subscribers": n})
}

func (h *Handler) mySubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserID(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authorization required")
		return
	}
	subs, err := h.svc.Subscriptions(r.Context(), userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": subs})
}

func (h *Handler) deleteVideoData(w http.ResponseWriter, r *http.Request) {
	videoID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.DeleteVideoData(r.Context(), videoID); err != nil {
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
	case errors.Is(err, service.ErrVideoNotFound),
		errors.Is(err, service.ErrCommentNotFound),
		errors.Is(err, service.ErrChannelNotFound),
		errors.Is(err, service.ErrNotSubscribed):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, err.Error())
	default:
		slog.Error("internal error", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
