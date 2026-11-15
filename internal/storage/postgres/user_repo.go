package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/perso/perso-mind/internal/domain"
)

type UserRepo struct {
	db *DB
}

func NewUserRepo(db *DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	query := `SELECT id, username, email, full_name, role, created_at, updated_at FROM users WHERE id = $1`
	var user domain.User
	var email, fullName *string

	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Username, &email, &fullName, &user.Role, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query user by id: %w", err)
	}

	if email != nil {
		user.Email = *email
	}
	if fullName != nil {
		user.FullName = *fullName
	}

	return &user, nil
}

func (r *UserRepo) GetOrCreateDefaultUser(ctx context.Context) (*domain.User, error) {
	defaultID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	user, err := r.GetByID(ctx, defaultID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}

	query := `
		INSERT INTO users (id, username, email, full_name, role)
		VALUES ($1, 'demo_buyer', 'buyer@perso.local', 'کاربر تستی پرسو', 'buyer')
		ON CONFLICT (id) DO UPDATE SET updated_at = NOW()
		RETURNING id, username, email, full_name, role, created_at, updated_at
	`
	var newUser domain.User
	var email, fullName *string
	err = r.db.Pool.QueryRow(ctx, query, defaultID).Scan(
		&newUser.ID, &newUser.Username, &email, &fullName, &newUser.Role, &newUser.CreatedAt, &newUser.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create default user: %w", err)
	}
	if email != nil {
		newUser.Email = *email
	}
	if fullName != nil {
		newUser.FullName = *fullName
	}
	return &newUser, nil
}
