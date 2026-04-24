package postgres

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExternalApplicationStatusReasonConstraints(t *testing.T) {
	db := openTestDB(t)
	resetDB(t, db)
	seedDB(t, db)

	var id int64
	err := db.QueryRow(context.Background(), `
		INSERT INTO external_applications (
			full_name,
			email,
			organisation_name,
			project_name,
			project_type_id,
			expected_results,
			is_payed,
			status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, "Ivan Ivanov", "ivan@example.com", "ACME", "Project X", 1, "Results", true, "PENDING").Scan(&id)
	if err != nil {
		t.Fatalf("insert pending application: %v", err)
	}

	testCases := []struct {
		name   string
		status string
		reason any
	}{
		{name: "accepted with rejection reason", status: "ACCEPTED", reason: "not allowed"},
		{name: "rejected without reason", status: "REJECTED", reason: nil},
		{name: "pending with rejection reason", status: "PENDING", reason: "not allowed"},
		{name: "rejected with blank reason", status: "REJECTED", reason: "   "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec(context.Background(), `
				UPDATE external_applications
				SET status = $2, rejection_reason = $3
				WHERE id = $1
			`, id, tc.status, tc.reason)
			if err == nil {
				t.Fatalf("expected constraint violation for status=%s reason=%v", tc.status, tc.reason)
			}
		})
	}
}

func TestRunMigrationsUsesAdvisoryLock(t *testing.T) {
	db := openTestDB(t)

	conn, err := db.Acquire(context.Background())
	if err != nil {
		t.Fatalf("db.Acquire() error = %v", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, migrationsAdvisoryLockKey); err != nil {
		t.Fatalf("acquire advisory lock: %v", err)
	}

	resultCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		resultCh <- RunMigrations(context.Background(), db)
	}()

	select {
	case err := <-resultCh:
		t.Fatalf("RunMigrations() returned before lock release: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationsAdvisoryLockKey); err != nil {
		t.Fatalf("release advisory lock: %v", err)
	}

	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("RunMigrations() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RunMigrations() did not finish after lock release")
	}

	wg.Wait()
}

func openTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	db, err := Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := RunMigrations(context.Background(), db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return db
}

func resetDB(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	_, err := db.Exec(context.Background(), `
		TRUNCATE TABLE external_applications, users, project_types RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("reset db: %v", err)
	}
}

func seedDB(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	if err := Seed(context.Background(), db); err != nil {
		t.Fatalf("seed db: %v", err)
	}
}
