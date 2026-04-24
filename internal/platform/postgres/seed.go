package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"Projects_Service/internal/domain"
	"Projects_Service/internal/platform/auth"
)

func Seed(ctx context.Context, db *pgxpool.Pool) error {
	adminHash, err := auth.HashPassword("admin123")
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	userHash, err := auth.HashPassword("user123")
	if err != nil {
		return fmt.Errorf("hash user password: %w", err)
	}

	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `
				INSERT INTO users (login, password_hash, role)
				VALUES ($1, $2, $3)
				ON CONFLICT (login) DO UPDATE
				SET password_hash = EXCLUDED.password_hash,
				    role = EXCLUDED.role,
				    updated_at = NOW()
			`,
			args: []any{"admin", adminHash, domain.RoleAdmin},
		},
		{
			query: `
				INSERT INTO users (login, password_hash, role)
				VALUES ($1, $2, $3)
				ON CONFLICT (login) DO UPDATE
				SET password_hash = EXCLUDED.password_hash,
				    role = EXCLUDED.role,
				    updated_at = NOW()
			`,
			args: []any{"user", userHash, domain.RoleUser},
		},
		{
			query: `
				INSERT INTO project_types (id, name)
				VALUES ($1, $2)
				ON CONFLICT (id) DO UPDATE
				SET name = EXCLUDED.name,
				    updated_at = NOW()
			`,
			args: []any{1, "Research"},
		},
		{
			query: `
				INSERT INTO project_types (id, name)
				VALUES ($1, $2)
				ON CONFLICT (id) DO UPDATE
				SET name = EXCLUDED.name,
				    updated_at = NOW()
			`,
			args: []any{2, "Implementation"},
		},
	}

	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("seed data: %w", err)
		}
	}

	return nil
}
