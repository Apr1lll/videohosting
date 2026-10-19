package model

import (
	"fmt"
	"time"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email,omitempty"`
	PasswordHash string    `json:"-"`
	Bio          string    `json:"bio"`
	Role         string    `json:"role"`
	AvatarFile   string    `json:"-"`
	AvatarURL    string    `json:"avatar_url"`
	HasAvatar    bool      `json:"has_avatar"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"-"`
}

// FillURLs проставляет ссылку на аватарку. Параметр v меняется при обновлении профиля,
// поэтому браузер не покажет старую картинку из кеша.
func (u *User) FillURLs() {
	u.AvatarURL = fmt.Sprintf("/users/%d/avatar?v=%d", u.ID, u.UpdatedAt.Unix())
	u.HasAvatar = u.AvatarFile != ""
}

// Public возвращает копию пользователя без приватных данных (email),
// чтобы её можно было показывать другим пользователям.
func (u User) Public() User {
	u.Email = ""
	return u
}

type UserPage struct {
	Items []User `json:"items"`
	Total int    `json:"total"`
	Page  int    `json:"page"`
	Limit int    `json:"limit"`
}

// AdminPatch — что администратор может поменять у пользователя. nil — поле не меняется.
type AdminPatch struct {
	Role     *string `json:"role"`
	Bio      *string `json:"bio"`
	Password *string `json:"password"`
}
