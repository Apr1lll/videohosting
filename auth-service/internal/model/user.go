package model

import "time"

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email,omitempty"`
	PasswordHash string    `json:"-"`
	Bio          string    `json:"bio"`
	CreatedAt    time.Time `json:"created_at"`
}

// Public возвращает копию пользователя без приватных данных (email),
// чтобы её можно было показывать другим пользователям.
func (u User) Public() User {
	u.Email = ""
	return u
}
