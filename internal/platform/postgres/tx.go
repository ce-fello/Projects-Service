package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	sqlcgen "Projects_Service/internal/platform/postgres/sqlc"
)

type txKey struct{}

type Transactor struct {
	db *pgxpool.Pool
}

func NewTransactor(db *pgxpool.Pool) *Transactor {
	return &Transactor{db: db}
}

func (t *Transactor) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	tx, err := t.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txCtx := context.WithValue(ctx, txKey{}, tx)
	if err := fn(txCtx); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("rollback tx: %v: %w", rollbackErr, err)
		}

		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

func queriesFromContext(ctx context.Context, db sqlcgen.DBTX) *sqlcgen.Queries {
	queries := sqlcgen.New(db)
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return queries.WithTx(tx)
	}

	return queries
}

type queryRunner interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func queryRunnerFromContext(ctx context.Context, db *pgxpool.Pool) queryRunner {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}

	return db
}
