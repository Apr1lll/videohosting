// Package service содержит бизнес-логику: регистрацию, вход, профиль, аватарки и администрирование.
package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
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
	ErrSelfAction         = errors.New("you cannot delete yourself or remove your own admin role")
)

// ValidationError — ошибка входных данных, её текст можно показать клиенту.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

const maxAvatarBytes = 2 << 20 // 2 МБ

// Claims — содержимое JWT-токена. Такая же структура используется в api-gateway.
type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type UserRepository interface {
	Create(ctx context.Context, u *model.User) error
	GetByID(ctx context.Context, id int64) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	UpdateBio(ctx context.Context, id int64, bio string) (*model.User, error)
	UpdateRole(ctx context.Context, id int64, role string) (*model.User, error)
	UpdatePassword(ctx context.Context, id int64, hash string) error
	SetAvatar(ctx context.Context, id int64, file string) (*model.User, error)
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, search string, limit, offset int) ([]model.User, int, error)
}

type FileStorage interface {
	Save(src io.Reader, ext string) (string, int64, error)
	Open(name string) (*os.File, error)
	Remove(name string) error
}

type UserDataCleaner interface {
	DeleteUserData(ctx context.Context, userID int64) error
}

type AuthService struct {
	repo      UserRepository
	avatars   FileStorage
	cleaner   UserDataCleaner
	admins    map[string]bool
	jwtSecret []byte
	tokenTTL  time.Duration
	dummyHash []byte
}

func NewAuthService(repo UserRepository, avatars FileStorage, cleaner UserDataCleaner,
	adminUsernames []string, secret string, ttl time.Duration) *AuthService {
	// Хеш-заглушка нужен, чтобы при входе с несуществующим email
	// время ответа было таким же, как при неверном пароле (защита от перебора email).
	dummy, _ := bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

	admins := make(map[string]bool, len(adminUsernames))
	for _, name := range adminUsernames {
		admins[strings.ToLower(name)] = true
	}
	return &AuthService{
		repo:      repo,
		avatars:   avatars,
		cleaner:   cleaner,
		admins:    admins,
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

func (s *AuthService) isConfiguredAdmin(username string) bool {
	return s.admins[strings.ToLower(username)]
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
	if err := validatePassword(password); err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	role := model.RoleUser
	if s.isConfiguredAdmin(username) {
		role = model.RoleAdmin
	}

	user := &model.User{Username: username, Email: email, PasswordHash: string(hash), Role: role}
	if err := s.repo.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, ErrUserExists
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	user.FillURLs()
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

	// Пользователь из списка ADMIN_USERNAMES получает права администратора при входе.
	if s.isConfiguredAdmin(user.Username) && user.Role != model.RoleAdmin {
		if user, err = s.repo.UpdateRole(ctx, user.ID, model.RoleAdmin); err != nil {
			return nil, fmt.Errorf("promote admin: %w", err)
		}
	}

	token, exp, err := s.issueToken(user)
	if err != nil {
		return nil, err
	}
	user.FillURLs()
	return &LoginResult{Token: token, ExpiresAt: exp, User: user}, nil
}

func (s *AuthService) GetUser(ctx context.Context, id int64) (*model.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	user.FillURLs()
	return user, nil
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
	if err != nil {
		return nil, err
	}
	user.FillURLs()
	return user, nil
}

// ---------- Аватарки ----------

// SetAvatar сохраняет новую аватарку (JPEG, PNG или WebP до 2 МБ) и удаляет старую.
func (s *AuthService) SetAvatar(ctx context.Context, id int64, src io.Reader) (*model.User, error) {
	data, err := io.ReadAll(io.LimitReader(src, maxAvatarBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAvatarBytes {
		return nil, &ValidationError{"avatar must be at most 2 MB"}
	}
	ext, ok := imageExt(data)
	if !ok {
		return nil, &ValidationError{"avatar must be a JPEG, PNG or WebP image"}
	}

	old, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	name, _, err := s.avatars.Save(bytes.NewReader(data), ext)
	if err != nil {
		return nil, fmt.Errorf("save avatar: %w", err)
	}
	user, err := s.repo.SetAvatar(ctx, id, name)
	if err != nil {
		_ = s.avatars.Remove(name)
		return nil, err
	}
	s.removeFile(old.AvatarFile)
	user.FillURLs()
	return user, nil
}

func (s *AuthService) DeleteAvatar(ctx context.Context, id int64) (*model.User, error) {
	old, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	user, err := s.repo.SetAvatar(ctx, id, "")
	if err != nil {
		return nil, err
	}
	s.removeFile(old.AvatarFile)
	user.FillURLs()
	return user, nil
}

// OpenAvatar открывает файл аватарки. Если аватарки нет — возвращает пользователя и nil-файл,
// тогда обработчик нарисует аватарку по умолчанию (первая буква имени).
func (s *AuthService) OpenAvatar(ctx context.Context, id int64) (*model.User, *os.File, error) {
	user, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if user.AvatarFile == "" {
		return user, nil, nil
	}
	f, err := s.avatars.Open(user.AvatarFile)
	if err != nil {
		return user, nil, nil
	}
	return user, f, nil
}

func (s *AuthService) removeFile(name string) {
	if name == "" {
		return
	}
	if err := s.avatars.Remove(name); err != nil {
		slog.Warn("failed to remove avatar", "file", name, "err", err)
	}
}

// imageExt определяет тип картинки по содержимому, а не по имени файла.
func imageExt(data []byte) (string, bool) {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/webp":
		return ".webp", true
	}
	return "", false
}

// ---------- Администрирование ----------

func (s *AuthService) AdminListUsers(ctx context.Context, search string, page, limit int) (*model.UserPage, error) {
	users, total, err := s.repo.List(ctx, strings.TrimSpace(search), limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	for i := range users {
		users[i].FillURLs()
	}
	return &model.UserPage{Items: users, Total: total, Page: page, Limit: limit}, nil
}

// AdminUpdateUser меняет роль, описание или пароль любого пользователя.
func (s *AuthService) AdminUpdateUser(ctx context.Context, actorID, id int64, p model.AdminPatch) (*model.User, error) {
	if _, err := s.GetUser(ctx, id); err != nil {
		return nil, err
	}

	if p.Role != nil {
		role := *p.Role
		if role != model.RoleUser && role != model.RoleAdmin {
			return nil, &ValidationError{"role must be 'user' or 'admin'"}
		}
		if id == actorID && role != model.RoleAdmin {
			return nil, ErrSelfAction
		}
		if _, err := s.repo.UpdateRole(ctx, id, role); err != nil {
			return nil, err
		}
	}
	if p.Bio != nil {
		if _, err := s.UpdateBio(ctx, id, *p.Bio); err != nil {
			return nil, err
		}
	}
	if p.Password != nil {
		if err := validatePassword(*p.Password); err != nil {
			return nil, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*p.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if err := s.repo.UpdatePassword(ctx, id, string(hash)); err != nil {
			return nil, err
		}
	}
	return s.GetUser(ctx, id)
}

// AdminDeleteUser удаляет пользователя вместе со всеми его данными во всех сервисах.
func (s *AuthService) AdminDeleteUser(ctx context.Context, actorID, id int64) error {
	if id == actorID {
		return ErrSelfAction
	}
	user, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	// Сначала чистим другие сервисы: если они недоступны, аккаунт останется
	// и удаление можно будет повторить, а не получить «осиротевшие» видео.
	if err := s.cleaner.DeleteUserData(ctx, id); err != nil {
		return fmt.Errorf("delete user data: %w", err)
	}
	if err := s.repo.Delete(ctx, id); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	s.removeFile(user.AvatarFile)
	return nil
}

// ---------- Вспомогательное ----------

func validatePassword(password string) error {
	if len(password) < 6 || len(password) > 72 {
		return &ValidationError{"password must be 6-72 characters long"}
	}
	return nil
}

func (s *AuthService) issueToken(u *model.User) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(s.tokenTTL)
	claims := Claims{
		Username: u.Username,
		Role:     u.Role,
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
