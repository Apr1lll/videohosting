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
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Identity — проверенные данные пользователя из токена.
type Identity struct {
	UserID   int64
	Username string
	Role     string
}

type Verifier struct {
	secret []byte
}

func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: []byte(secret)}
}

// Verify разбирает заголовок "Authorization: Bearer <token>" и возвращает данные пользователя.
func (v *Verifier) Verify(header string) (*Identity, error) {
	raw, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || raw == "" {
		return nil, ErrInvalidToken
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		return v.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return nil, ErrInvalidToken
	}
	role := claims.Role
	if role == "" {
		role = "user" // токены, выданные до появления ролей
	}
	return &Identity{UserID: userID, Username: claims.Username, Role: role}, nil
}
