package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"Projects_Service/internal/domain"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) GetByLogin(ctx context.Context, login string) (domain.User, error) {
	var user domain.User

	err := dbtx(ctx, r.db).QueryRowContext(ctx, `
		SELECT id, login, password_hash, role
		FROM users
		WHERE login = $1
	`, login).Scan(&user.ID, &user.Login, &user.PasswordHash, &user.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, domain.ErrNotFound
		}

		return domain.User{}, fmt.Errorf("get user by login: %w", err)
	}

	return user, nil
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (domain.User, error) {
	var user domain.User

	err := dbtx(ctx, r.db).QueryRowContext(ctx, `
		SELECT id, login, password_hash, role
		FROM users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.Login, &user.PasswordHash, &user.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, domain.ErrNotFound
		}

		return domain.User{}, fmt.Errorf("get user by id: %w", err)
	}

	return user, nil
}

type ProjectTypeRepository struct {
	db *sql.DB
}

func NewProjectTypeRepository(db *sql.DB) *ProjectTypeRepository {
	return &ProjectTypeRepository{db: db}
}

func (r *ProjectTypeRepository) List(ctx context.Context) ([]domain.ProjectType, error) {
	rows, err := dbtx(ctx, r.db).QueryContext(ctx, `
		SELECT id, name
		FROM project_types
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("list project types: %w", err)
	}
	defer rows.Close()

	projectTypes := make([]domain.ProjectType, 0)
	for rows.Next() {
		var projectType domain.ProjectType
		if err := rows.Scan(&projectType.ID, &projectType.Name); err != nil {
			return nil, fmt.Errorf("scan project type: %w", err)
		}

		projectTypes = append(projectTypes, projectType)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project types: %w", err)
	}

	return projectTypes, nil
}

func (r *ProjectTypeRepository) Exists(ctx context.Context, id int64) (bool, error) {
	var exists bool
	if err := dbtx(ctx, r.db).QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM project_types WHERE id = $1)
	`, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("check project type exists: %w", err)
	}

	return exists, nil
}

type ExternalApplicationRepository struct {
	db *sql.DB
}

func NewExternalApplicationRepository(db *sql.DB) *ExternalApplicationRepository {
	return &ExternalApplicationRepository{db: db}
}

func (r *ExternalApplicationRepository) Create(ctx context.Context, input domain.CreateExternalApplicationInput) (int64, error) {
	var id int64
	err := dbtx(ctx, r.db).QueryRowContext(ctx, `
		INSERT INTO external_applications (
			full_name,
			email,
			phone,
			organisation_name,
			organisation_url,
			project_name,
			project_type_id,
			expected_results,
			is_payed,
			additional_information,
			status
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
		RETURNING id
	`,
		input.FullName,
		input.Email,
		input.Phone,
		input.OrganisationName,
		input.OrganisationURL,
		input.ProjectName,
		input.TypeID,
		input.ExpectedResults,
		input.IsPayed,
		input.AdditionalInformation,
		domain.StatusPending,
	).Scan(&id)
	if err != nil {
		return 0, mapDatabaseError("create external application", err)
	}

	return id, nil
}

func (r *ExternalApplicationRepository) GetByID(ctx context.Context, id int64) (domain.ExternalApplication, error) {
	return r.getByID(ctx, id, false)
}

func (r *ExternalApplicationRepository) GetByIDForUpdate(ctx context.Context, id int64) (domain.ExternalApplication, error) {
	return r.getByID(ctx, id, true)
}

func (r *ExternalApplicationRepository) getByID(ctx context.Context, id int64, forUpdate bool) (domain.ExternalApplication, error) {
	query := `
		SELECT
			ea.id,
			ea.full_name,
			ea.email,
			ea.phone,
			ea.organisation_name,
			ea.organisation_url,
			ea.project_name,
			ea.project_type_id,
			pt.name,
			ea.expected_results,
			ea.is_payed,
			ea.additional_information,
			ea.rejection_reason,
			ea.status,
			ea.created_at,
			ea.updated_at
		FROM external_applications ea
		JOIN project_types pt ON pt.id = ea.project_type_id
		WHERE ea.id = $1
	`
	if forUpdate {
		query += " FOR UPDATE"
	}

	var application domain.ExternalApplication
	err := dbtx(ctx, r.db).QueryRowContext(ctx, query, id).Scan(
		&application.ID,
		&application.FullName,
		&application.Email,
		&application.Phone,
		&application.OrganisationName,
		&application.OrganisationURL,
		&application.ProjectName,
		&application.ProjectTypeID,
		&application.TypeName,
		&application.ExpectedResults,
		&application.IsPayed,
		&application.AdditionalInformation,
		&application.RejectionReason,
		&application.Status,
		&application.CreatedAt,
		&application.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ExternalApplication{}, domain.ErrNotFound
		}

		return domain.ExternalApplication{}, fmt.Errorf("get external application: %w", err)
	}

	return application, nil
}

func (r *ExternalApplicationRepository) UpdateDecision(ctx context.Context, id int64, status domain.ExternalApplicationStatus, reason *string) error {
	result, err := dbtx(ctx, r.db).ExecContext(ctx, `
		UPDATE external_applications
		SET status = $2,
		    rejection_reason = $3,
		    updated_at = $4
		WHERE id = $1
	`, id, status, reason, time.Now().UTC())
	if err != nil {
		return mapDatabaseError("update decision", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("decision rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrNotFound
	}

	return nil
}

func mapDatabaseError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return domain.ValidationError{Message: "project type not found"}
		case "23514":
			return domain.ValidationError{Message: "database constraint violation"}
		}
	}

	return fmt.Errorf("%s: %w", operation, err)
}

func (r *ExternalApplicationRepository) List(ctx context.Context, filter domain.ListExternalApplicationsFilter) (domain.ExternalApplicationList, error) {
	where := []string{"1 = 1"}
	args := make([]any, 0, 6)

	if filter.ActiveOnly != nil {
		if *filter.ActiveOnly {
			args = append(args, domain.StatusPending)
			where = append(where, fmt.Sprintf("ea.status = $%d", len(args)))
		} else {
			args = append(args, domain.StatusAccepted, domain.StatusRejected)
			where = append(where, fmt.Sprintf("ea.status IN ($%d, $%d)", len(args)-1, len(args)))
		}
	}

	if filter.Search != "" {
		args = append(args, "%"+strings.ToLower(filter.Search)+"%")
		where = append(where, fmt.Sprintf("(LOWER(ea.project_name) LIKE $%d OR LOWER(ea.full_name) LIKE $%d)", len(args), len(args)))
	}

	if filter.ProjectTypeID != nil {
		args = append(args, *filter.ProjectTypeID)
		where = append(where, fmt.Sprintf("ea.project_type_id = $%d", len(args)))
	}

	whereSQL := strings.Join(where, " AND ")

	var count int64
	countQuery := `
		SELECT COUNT(*)
		FROM external_applications ea
		WHERE ` + whereSQL
	if err := dbtx(ctx, r.db).QueryRowContext(ctx, countQuery, args...).Scan(&count); err != nil {
		return domain.ExternalApplicationList{}, fmt.Errorf("count external applications: %w", err)
	}

	sortDirection := "DESC"
	if filter.SortByDateUpdated == domain.SortAsc {
		sortDirection = "ASC"
	}

	args = append(args, filter.Limit, filter.Offset)
	listQuery := `
		SELECT
			ea.id,
			ea.project_name,
			pt.name,
			ea.full_name,
			ea.organisation_name,
			ea.updated_at,
			ea.status,
			ea.rejection_reason
		FROM external_applications ea
		JOIN project_types pt ON pt.id = ea.project_type_id
		WHERE ` + whereSQL + `
		ORDER BY ea.updated_at ` + sortDirection + `
		LIMIT $` + fmt.Sprint(len(args)-1) + ` OFFSET $` + fmt.Sprint(len(args))

	rows, err := dbtx(ctx, r.db).QueryContext(ctx, listQuery, args...)
	if err != nil {
		return domain.ExternalApplicationList{}, fmt.Errorf("list external applications: %w", err)
	}
	defer rows.Close()

	applications := make([]domain.ExternalApplicationPreview, 0)
	for rows.Next() {
		var preview domain.ExternalApplicationPreview
		if err := rows.Scan(
			&preview.ExternalApplicationID,
			&preview.ProjectName,
			&preview.TypeName,
			&preview.Initiator,
			&preview.OrganisationName,
			&preview.DateUpdated,
			&preview.Status,
			&preview.RejectionMessage,
		); err != nil {
			return domain.ExternalApplicationList{}, fmt.Errorf("scan external application preview: %w", err)
		}

		applications = append(applications, preview)
	}

	if err := rows.Err(); err != nil {
		return domain.ExternalApplicationList{}, fmt.Errorf("iterate external application preview: %w", err)
	}

	return domain.ExternalApplicationList{
		Count:        count,
		Applications: applications,
	}, nil
}
