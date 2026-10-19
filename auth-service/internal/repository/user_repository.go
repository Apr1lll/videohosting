// Package repository — слой доступа к данным (PostgreSQL).
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"videohosting/auth-service/internal/model"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("duplicate")
)

const uniqueViolationCode = "23505"

const userColumns = `id, username, email, password_hash, bio, role, avatar_file, created_at, updated_at`

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

// Create сохраняет нового пользователя и заполняет ID и даты.
func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	err := r.db.QueryRow(ctx,
		`INSERT INTO users (username, email, password_hash, role)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at, updated_at`,
		u.Username, u.Email, u.PasswordHash, u.Role,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
		return ErrDuplicate
	}
	return err
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (*model.User, error) {
	return scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	return scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email))
}

func (r *UserRepository) UpdateBio(ctx context.Context, id int64, bio string) (*model.User, error) {
	return scanUser(r.db.QueryRow(ctx,
		`UPDATE users SET bio = $2, updated_at = NOW() WHERE id = $1 RETURNING `+userColumns, id, bio))
}

func (r *UserRepository) UpdateRole(ctx context.Context, id int64, role string) (*model.User, error) {
	return scanUser(r.db.QueryRow(ctx,
		`UPDATE users SET role = $2, updated_at = NOW() WHERE id = $1 RETURNING `+userColumns, id, role))
}

func (r *UserRepository) UpdatePassword(ctx context.Context, id int64, hash string) error {
	tag, err := r.db.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1`, id, hash)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetAvatar меняет файл аватарки и возвращает обновлённого пользователя.
func (r *UserRepository) SetAvatar(ctx context.Context, id int64, file string) (*model.User, error) {
	return scanUser(r.db.QueryRow(ctx,
		`UPDATE users SET avatar_file = $2, updated_at = NOW() WHERE id = $1 RETURNING `+userColumns, id, file))
}

func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// List — список пользователей для админ-панели с поиском по имени и email.
func (r *UserRepository) List(ctx context.Context, search string, limit, offset int) ([]model.User, int, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+userColumns+`, COUNT(*) OVER()
		 FROM users
		 WHERE $1 = '' OR username ILIKE '%' || $1 || '%' OR email ILIKE '%' || $1 || '%'
		 ORDER BY id DESC
		 LIMIT $2 OFFSET $3`, search, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]model.User, 0, limit)
	total := 0
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.Role,
			&u.AvatarFile, &u.CreatedAt, &u.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

func scanUser(row pgx.Row) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.Role,
		&u.AvatarFile, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
