// Package service содержит бизнес-логику: регистрацию, вход и работу с профилем.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"videohosting/auth-service/internal/model"
	"videohosting/auth-service/internal/repository"
)

var (
	ErrUserExists         = errors.New("user with this email or username already exists")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNotFound           = errors.New("user not found")
)

// ValidationError — ошибка входных данных, её текст можно показать клиенту.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

// Claims — содержимое JWT-токена. Такая же структура используется в api-gateway.
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type UserRepository interface {
	Create(ctx context.Context, u *model.User) error
	GetByID(ctx context.Context, id int64) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	UpdateBio(ctx context.Context, id int64, bio string) (*model.User, error)
}

type AuthService struct {
	repo      UserRepository
	jwtSecret []byte
	tokenTTL  time.Duration
	dummyHash []byte
}

func NewAuthService(repo UserRepository, secret string, ttl time.Duration) *AuthService {
	// Хеш-заглушка нужен, чтобы при входе с несуществующим email
	// время ответа было таким же, как при неверном пароле (защита от перебора email).
	dummy, _ := bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)
	return &AuthService{
		repo:      repo,
		jwtSecret: []byte(secret),
		tokenTTL:  ttl,
		dummyHash: dummy,
	}
}

type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      *model.User `json:"user"`
}

func (s *AuthService) Register(ctx context.Context, username, email, password string) (*model.User, error) {
	username = strings.TrimSpace(username)
	email = strings.ToLower(strings.TrimSpace(email))

	if !usernameRe.MatchString(username) {
		return nil, &ValidationError{"username must be 3-32 characters: latin letters, digits or _"}
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, &ValidationError{"invalid email"}
	}
	if len(password) < 6 || len(password) > 72 {
		return nil, &ValidationError{"password must be 6-72 characters long"}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &model.User{Username: username, Email: email, PasswordHash: string(hash)}
	if err := s.repo.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, ErrUserExists
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	user, err := s.repo.GetByEmail(ctx, email)
	if errors.Is(err, repository.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	token, exp, err := s.issueToken(user)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Token: token, ExpiresAt: exp, User: user}, nil
}

func (s *AuthService) GetUser(ctx context.Context, id int64) (*model.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	return user, err
}

func (s *AuthService) UpdateBio(ctx context.Context, id int64, bio string) (*model.User, error) {
	bio = strings.TrimSpace(bio)
	if utf8.RuneCountInString(bio) > 500 {
		return nil, &ValidationError{"bio must be at most 500 characters"}
	}
	user, err := s.repo.UpdateBio(ctx, id, bio)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	return user, err
}

func (s *AuthService) issueToken(u *model.User) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(s.tokenTTL)
	claims := Claims{
		Username: u.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			Issuer:    "videohosting-auth",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return token, exp, nil
}
