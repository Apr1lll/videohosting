package httpx

import (
	"net/http"
	"strconv"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// Pagination читает параметры ?page=&limit= и подставляет разумные значения по умолчанию.
func Pagination(r *http.Request) (page, limit int) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	limit, err = strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return page, limit
}
