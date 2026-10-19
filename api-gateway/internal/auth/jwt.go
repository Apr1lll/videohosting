// Package auth проверяет JWT-токены, выданные auth-service.
package auth

import (
	"errors"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid or expired token")

// Claims должны совпадать со структурой в auth-service.
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type Verifier struct {
	secret []byte
}

func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: []byte(secret)}
}

// Verify разбирает заголовок "Authorization: Bearer <token>" и возвращает ID и имя пользователя.
func (v *Verifier) Verify(header string) (userID int64, username string, err error) {
	raw, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || raw == "" {
		return 0, "", ErrInvalidToken
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		return v.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return 0, "", ErrInvalidToken
	}

	userID, err = strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return 0, "", ErrInvalidToken
	}
	return userID, claims.Username, nil
}
