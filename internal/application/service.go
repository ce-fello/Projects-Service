package application

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"

	"Projects_Service/internal/domain"
	"Projects_Service/internal/platform/auth"
)

var phonePattern = regexp.MustCompile(`^\+7 \(\d{3}\) \d{3}-\d{2}-\d{2}$`)

type UserRepository interface {
	GetByLogin(ctx context.Context, login string) (domain.User, error)
}

type ProjectTypeRepository interface {
	List(ctx context.Context) ([]domain.ProjectType, error)
	Exists(ctx context.Context, id int64) (bool, error)
}

type ExternalApplicationRepository interface {
	Create(ctx context.Context, input domain.CreateExternalApplicationInput) (int64, error)
	GetByID(ctx context.Context, id int64) (domain.ExternalApplication, error)
	GetByIDForUpdate(ctx context.Context, id int64) (domain.ExternalApplication, error)
	UpdateDecision(ctx context.Context, id int64, status domain.ExternalApplicationStatus, reason *string) error
	List(ctx context.Context, filter domain.ListExternalApplicationsFilter) (domain.ExternalApplicationList, error)
}

type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(context.Context) error) error
}

type Service struct {
	users        UserRepository
	projectTypes ProjectTypeRepository
	applications ExternalApplicationRepository
	transactor   Transactor
	tokenManager *auth.TokenManager
}

func NewService(
	users UserRepository,
	projectTypes ProjectTypeRepository,
	applications ExternalApplicationRepository,
	transactor Transactor,
	tokenManager *auth.TokenManager,
) *Service {
	return &Service{
		users:        users,
		projectTypes: projectTypes,
		applications: applications,
		transactor:   transactor,
		tokenManager: tokenManager,
	}
}

func (s *Service) Login(ctx context.Context, login, password string) (string, error) {
	login = strings.TrimSpace(login)
	password = strings.TrimSpace(password)
	if login == "" || password == "" {
		return "", domain.ValidationError{Message: "login and password are required"}
	}

	user, err := s.users.GetByLogin(ctx, login)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", domain.ErrInvalidCredentials
		}

		return "", err
	}

	if err := auth.VerifyPassword(user.PasswordHash, password); err != nil {
		return "", domain.ErrInvalidCredentials
	}

	token, err := s.tokenManager.Generate(user)
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	return token, nil
}

func (s *Service) ListProjectTypes(ctx context.Context) ([]domain.ProjectType, error) {
	return s.projectTypes.List(ctx)
}

func (s *Service) CreateExternalApplication(ctx context.Context, input domain.CreateExternalApplicationInput) (int64, error) {
	normalizeCreateInput(&input)
	if err := validateCreateInput(input); err != nil {
		return 0, err
	}

	exists, err := s.projectTypes.Exists(ctx, input.TypeID)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, domain.ValidationError{Message: "project type not found"}
	}

	return s.applications.Create(ctx, input)
}

func (s *Service) GetExternalApplication(ctx context.Context, actor domain.Actor, id int64) (domain.ExternalApplication, error) {
	if err := authorizeAdmin(actor); err != nil {
		return domain.ExternalApplication{}, err
	}

	return s.applications.GetByID(ctx, id)
}

func (s *Service) AcceptExternalApplication(ctx context.Context, actor domain.Actor, id int64) error {
	if err := authorizeAdmin(actor); err != nil {
		return err
	}

	return s.changeDecision(ctx, id, domain.StatusAccepted, nil)
}

func (s *Service) RejectExternalApplication(ctx context.Context, actor domain.Actor, id int64, reason string) error {
	if err := authorizeAdmin(actor); err != nil {
		return err
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return domain.ValidationError{Message: "reason is required"}
	}
	if len(reason) > 750 {
		return domain.ValidationError{Message: "reason exceeds 750 characters"}
	}

	return s.changeDecision(ctx, id, domain.StatusRejected, &reason)
}

func (s *Service) ListExternalApplications(ctx context.Context, actor domain.Actor, filter domain.ListExternalApplicationsFilter) (domain.ExternalApplicationList, error) {
	if err := authorizeAdmin(actor); err != nil {
		return domain.ExternalApplicationList{}, err
	}

	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Offset < 0 {
		return domain.ExternalApplicationList{}, domain.ValidationError{Message: "offset must be non-negative"}
	}
	if filter.Limit > 100 {
		return domain.ExternalApplicationList{}, domain.ValidationError{Message: "limit must be less or equal to 100"}
	}
	if filter.SortByDateUpdated == "" {
		filter.SortByDateUpdated = domain.SortDesc
	}
	if filter.SortByDateUpdated != domain.SortAsc && filter.SortByDateUpdated != domain.SortDesc {
		return domain.ExternalApplicationList{}, domain.ValidationError{Message: "sortByDateUpdated must be ASC or DESC"}
	}
	filter.Search = strings.TrimSpace(filter.Search)

	if filter.ProjectTypeID != nil {
		exists, err := s.projectTypes.Exists(ctx, *filter.ProjectTypeID)
		if err != nil {
			return domain.ExternalApplicationList{}, err
		}
		if !exists {
			return domain.ExternalApplicationList{}, domain.ValidationError{Message: "project type not found"}
		}
	}

	return s.applications.List(ctx, filter)
}

func (s *Service) changeDecision(ctx context.Context, id int64, status domain.ExternalApplicationStatus, reason *string) error {
	return s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		application, err := s.applications.GetByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if application.Status != domain.StatusPending {
			return domain.ErrInvalidState
		}

		return s.applications.UpdateDecision(txCtx, id, status, reason)
	})
}

func validateCreateInput(input domain.CreateExternalApplicationInput) error {
	switch {
	case strings.TrimSpace(input.FullName) == "":
		return domain.ValidationError{Message: "fullName is required"}
	case len(input.FullName) > 150:
		return domain.ValidationError{Message: "fullName exceeds 150 characters"}
	case strings.TrimSpace(input.Email) == "":
		return domain.ValidationError{Message: "email is required"}
	case len(input.Email) > 150:
		return domain.ValidationError{Message: "email exceeds 150 characters"}
	case !isValidEmail(input.Email):
		return domain.ValidationError{Message: "email has invalid format"}
	case strings.TrimSpace(input.OrganisationName) == "":
		return domain.ValidationError{Message: "organisationName is required"}
	case len(input.OrganisationName) > 150:
		return domain.ValidationError{Message: "organisationName exceeds 150 characters"}
	case strings.TrimSpace(input.ProjectName) == "":
		return domain.ValidationError{Message: "projectName is required"}
	case len(input.ProjectName) > 150:
		return domain.ValidationError{Message: "projectName exceeds 150 characters"}
	case input.TypeID <= 0:
		return domain.ValidationError{Message: "typeId must be positive"}
	case strings.TrimSpace(input.ExpectedResults) == "":
		return domain.ValidationError{Message: "expectedResults is required"}
	case len(input.ExpectedResults) > 1500:
		return domain.ValidationError{Message: "expectedResults exceeds 1500 characters"}
	}

	if input.Phone != nil {
		if !phonePattern.MatchString(*input.Phone) {
			return domain.ValidationError{Message: "phone has invalid format"}
		}
	}

	if input.OrganisationURL != nil && len(*input.OrganisationURL) > 2048 {
		return domain.ValidationError{Message: "organisationUrl exceeds 2048 characters"}
	}
	if input.OrganisationURL != nil && !isValidHTTPURL(*input.OrganisationURL) {
		return domain.ValidationError{Message: "organisationUrl has invalid format"}
	}

	if input.AdditionalInformation != nil && len(*input.AdditionalInformation) > 1500 {
		return domain.ValidationError{Message: "additionalInformation exceeds 1500 characters"}
	}

	return nil
}

func normalizeCreateInput(input *domain.CreateExternalApplicationInput) {
	input.FullName = strings.TrimSpace(input.FullName)
	input.Email = strings.TrimSpace(input.Email)
	input.OrganisationName = strings.TrimSpace(input.OrganisationName)
	input.ProjectName = strings.TrimSpace(input.ProjectName)
	input.ExpectedResults = strings.TrimSpace(input.ExpectedResults)
	input.Phone = trimOptional(input.Phone)
	input.OrganisationURL = trimOptional(input.OrganisationURL)
	input.AdditionalInformation = trimOptional(input.AdditionalInformation)
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}

func isValidEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	if err != nil {
		return false
	}

	return address.Address == value
}

func isValidHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}

	return parsed.Host != ""
}

func authorizeAdmin(actor domain.Actor) error {
	if actor.UserID <= 0 {
		return domain.ErrUnauthorized
	}
	if actor.Role != domain.RoleAdmin {
		return domain.ErrForbidden
	}

	return nil
}
