package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"Projects_Service/internal/domain"
	sqlcgen "Projects_Service/internal/platform/postgres/sqlc"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) GetByLogin(ctx context.Context, login string) (domain.User, error) {
	row, err := queriesFromContext(ctx, r.db).GetUserByLogin(ctx, login)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrNotFound
		}

		return domain.User{}, fmt.Errorf("get user by login: %w", err)
	}

	return domain.User{
		ID:           row.ID,
		Login:        row.Login,
		PasswordHash: row.PasswordHash,
		Role:         domain.Role(row.Role),
	}, nil
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (domain.User, error) {
	row, err := queriesFromContext(ctx, r.db).GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrNotFound
		}

		return domain.User{}, fmt.Errorf("get user by id: %w", err)
	}

	return domain.User{
		ID:           row.ID,
		Login:        row.Login,
		PasswordHash: row.PasswordHash,
		Role:         domain.Role(row.Role),
	}, nil
}

type ProjectTypeRepository struct {
	db *pgxpool.Pool
}

func NewProjectTypeRepository(db *pgxpool.Pool) *ProjectTypeRepository {
	return &ProjectTypeRepository{db: db}
}

func (r *ProjectTypeRepository) List(ctx context.Context) ([]domain.ProjectType, error) {
	rows, err := queriesFromContext(ctx, r.db).ListProjectTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list project types: %w", err)
	}

	projectTypes := make([]domain.ProjectType, 0, len(rows))
	for _, row := range rows {
		projectTypes = append(projectTypes, domain.ProjectType{
			ID:   row.ID,
			Name: row.Name,
		})
	}

	return projectTypes, nil
}

func (r *ProjectTypeRepository) Exists(ctx context.Context, id int64) (bool, error) {
	exists, err := queriesFromContext(ctx, r.db).ExistsProjectType(ctx, id)
	if err != nil {
		return false, fmt.Errorf("check project type exists: %w", err)
	}

	return exists, nil
}

type ExternalApplicationRepository struct {
	db *pgxpool.Pool
}

func NewExternalApplicationRepository(db *pgxpool.Pool) *ExternalApplicationRepository {
	return &ExternalApplicationRepository{db: db}
}

func (r *ExternalApplicationRepository) Create(ctx context.Context, input domain.CreateExternalApplicationInput) (int64, error) {
	id, err := queriesFromContext(ctx, r.db).CreateExternalApplication(ctx, sqlcgen.CreateExternalApplicationParams{
		FullName:              input.FullName,
		Email:                 input.Email,
		Phone:                 input.Phone,
		OrganisationName:      input.OrganisationName,
		OrganisationUrl:       input.OrganisationURL,
		ProjectName:           input.ProjectName,
		ProjectTypeID:         input.TypeID,
		ExpectedResults:       input.ExpectedResults,
		IsPayed:               input.IsPayed,
		AdditionalInformation: input.AdditionalInformation,
		Status:                string(domain.StatusPending),
	})
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
	queries := queriesFromContext(ctx, r.db)
	var (
		application domain.ExternalApplication
		err         error
	)
	if forUpdate {
		var row sqlcgen.GetExternalApplicationForUpdateRow
		row, err = queries.GetExternalApplicationForUpdate(ctx, id)
		if err == nil {
			application = mapExternalApplicationForUpdate(row)
		}
	} else {
		var row sqlcgen.GetExternalApplicationRow
		row, err = queries.GetExternalApplication(ctx, id)
		if err == nil {
			application = mapExternalApplication(row)
		}
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ExternalApplication{}, domain.ErrNotFound
		}

		return domain.ExternalApplication{}, fmt.Errorf("get external application: %w", err)
	}

	return application, nil
}

func (r *ExternalApplicationRepository) UpdateDecision(ctx context.Context, id int64, status domain.ExternalApplicationStatus, reason *string) error {
	rowsAffected, err := queriesFromContext(ctx, r.db).UpdateDecision(ctx, sqlcgen.UpdateDecisionParams{
		ID:              id,
		Status:          string(status),
		RejectionReason: reason,
		UpdatedAt:       time.Now().UTC(),
	})
	if err != nil {
		return mapDatabaseError("update decision", err)
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
	builder := sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
	countBuilder := applyExternalApplicationsFilter(
		builder.Select("COUNT(*)").From("external_applications ea"),
		filter,
	)
	countQuery, countArgs, err := countBuilder.ToSql()
	if err != nil {
		return domain.ExternalApplicationList{}, fmt.Errorf("build count external applications: %w", err)
	}

	var count int64
	if err := queryRunnerFromContext(ctx, r.db).QueryRow(ctx, countQuery, countArgs...).Scan(&count); err != nil {
		return domain.ExternalApplicationList{}, fmt.Errorf("count external applications: %w", err)
	}

	sortDirection := "DESC"
	if filter.SortByDateUpdated == domain.SortAsc {
		sortDirection = "ASC"
	}

	listBuilder := applyExternalApplicationsFilter(
		builder.Select(
			"ea.id",
			"ea.project_name",
			"pt.name",
			"ea.full_name",
			"ea.organisation_name",
			"ea.updated_at",
			"ea.status",
			"ea.rejection_reason",
		).
			From("external_applications ea").
			Join("project_types pt ON pt.id = ea.project_type_id"),
		filter,
	).OrderBy("ea.updated_at " + sortDirection).
		Limit(uint64(filter.Limit)).
		Offset(uint64(filter.Offset))

	listQuery, listArgs, err := listBuilder.ToSql()
	if err != nil {
		return domain.ExternalApplicationList{}, fmt.Errorf("build list external applications: %w", err)
	}

	rows, err := queryRunnerFromContext(ctx, r.db).Query(ctx, listQuery, listArgs...)
	if err != nil {
		return domain.ExternalApplicationList{}, fmt.Errorf("list external applications: %w", err)
	}
	defer rows.Close()

	applications, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ExternalApplicationPreview, error) {
		var preview domain.ExternalApplicationPreview
		if err := row.Scan(
			&preview.ExternalApplicationID,
			&preview.ProjectName,
			&preview.TypeName,
			&preview.Initiator,
			&preview.OrganisationName,
			&preview.DateUpdated,
			&preview.Status,
			&preview.RejectionMessage,
		); err != nil {
			return domain.ExternalApplicationPreview{}, err
		}

		return preview, nil
	})
	if err != nil {
		return domain.ExternalApplicationList{}, fmt.Errorf("scan external application preview: %w", err)
	}

	return domain.ExternalApplicationList{
		Count:        count,
		Applications: applications,
	}, nil
}

func applyExternalApplicationsFilter(builder sq.SelectBuilder, filter domain.ListExternalApplicationsFilter) sq.SelectBuilder {
	if filter.ActiveOnly != nil {
		if *filter.ActiveOnly {
			builder = builder.Where(sq.Eq{"ea.status": string(domain.StatusPending)})
		} else {
			builder = builder.Where(sq.Eq{"ea.status": []string{string(domain.StatusAccepted), string(domain.StatusRejected)}})
		}
	}

	if filter.Search != "" {
		search := "%" + strings.ToLower(filter.Search) + "%"
		builder = builder.Where(sq.Or{
			sq.Expr("LOWER(ea.project_name) LIKE ?", search),
			sq.Expr("LOWER(ea.full_name) LIKE ?", search),
		})
	}

	if filter.ProjectTypeID != nil {
		builder = builder.Where(sq.Eq{"ea.project_type_id": *filter.ProjectTypeID})
	}

	return builder
}

func mapExternalApplication(row sqlcgen.GetExternalApplicationRow) domain.ExternalApplication {
	return domain.ExternalApplication{
		ID:                    row.ID,
		FullName:              row.FullName,
		Email:                 row.Email,
		Phone:                 row.Phone,
		OrganisationName:      row.OrganisationName,
		OrganisationURL:       row.OrganisationUrl,
		ProjectName:           row.ProjectName,
		ProjectTypeID:         row.ProjectTypeID,
		TypeName:              row.TypeName,
		ExpectedResults:       row.ExpectedResults,
		IsPayed:               row.IsPayed,
		AdditionalInformation: row.AdditionalInformation,
		RejectionReason:       row.RejectionReason,
		Status:                domain.ExternalApplicationStatus(row.Status),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
}

func mapExternalApplicationForUpdate(row sqlcgen.GetExternalApplicationForUpdateRow) domain.ExternalApplication {
	return domain.ExternalApplication{
		ID:                    row.ID,
		FullName:              row.FullName,
		Email:                 row.Email,
		Phone:                 row.Phone,
		OrganisationName:      row.OrganisationName,
		OrganisationURL:       row.OrganisationUrl,
		ProjectName:           row.ProjectName,
		ProjectTypeID:         row.ProjectTypeID,
		TypeName:              row.TypeName,
		ExpectedResults:       row.ExpectedResults,
		IsPayed:               row.IsPayed,
		AdditionalInformation: row.AdditionalInformation,
		RejectionReason:       row.RejectionReason,
		Status:                domain.ExternalApplicationStatus(row.Status),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
}
