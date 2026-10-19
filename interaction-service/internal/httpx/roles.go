package httpx

import "net/http"

// IsAdmin — true, если gateway пометил запрос как пришедший от администратора.
// Заголовок X-User-Role ставит только gateway после проверки JWT; от клиента он отбрасывается.
func IsAdmin(r *http.Request) bool {
	return r.Header.Get("X-User-Role") == "admin"
}
