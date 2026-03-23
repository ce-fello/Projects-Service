package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"Projects_Service/internal/domain"
	"Projects_Service/internal/platform/auth"
)

type fakeUserRepository struct {
	user domain.User
	err  error
}

func (r fakeUserRepository) GetByLogin(context.Context, string) (domain.User, error) {
	return r.user, r.err
}

type fakeProjectTypeRepository struct {
	projectTypes []domain.ProjectType
	exists       bool
	err          error
}

func (r fakeProjectTypeRepository) List(context.Context) ([]domain.ProjectType, error) {
	return r.projectTypes, r.err
}

func (r fakeProjectTypeRepository) Exists(context.Context, int64) (bool, error) {
	return r.exists, r.err
}

type fakeApplicationRepository struct {
	createInput    domain.CreateExternalApplicationInput
	createID       int64
	createErr      error
	application    domain.ExternalApplication
	getErr         error
	updateErr      error
	listResponse   domain.ExternalApplicationList
	listErr        error
	listFilter     domain.ListExternalApplicationsFilter
	decision       domain.ExternalApplicationStatus
	decisionReason *string
}

func (r *fakeApplicationRepository) Create(_ context.Context, input domain.CreateExternalApplicationInput) (int64, error) {
	r.createInput = input
	return r.createID, r.createErr
}

func (r *fakeApplicationRepository) GetByID(context.Context, int64) (domain.ExternalApplication, error) {
	return r.application, r.getErr
}

func (r *fakeApplicationRepository) GetByIDForUpdate(context.Context, int64) (domain.ExternalApplication, error) {
	return r.application, r.getErr
}

func (r *fakeApplicationRepository) UpdateDecision(_ context.Context, _ int64, status domain.ExternalApplicationStatus, reason *string) error {
	r.decision = status
	r.decisionReason = reason
	return r.updateErr
}

func (r *fakeApplicationRepository) List(_ context.Context, filter domain.ListExternalApplicationsFilter) (domain.ExternalApplicationList, error) {
	r.listFilter = filter
	return r.listResponse, r.listErr
}

type noopTransactor struct{}

func (noopTransactor) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func newTestService(t *testing.T, users fakeUserRepository, projectTypes fakeProjectTypeRepository, applications *fakeApplicationRepository) *Service {
	t.Helper()

	return NewService(
		users,
		projectTypes,
		applications,
		noopTransactor{},
		auth.NewTokenManager("test-secret"),
	)
}

func hashedPassword(t *testing.T, password string) string {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	return hash
}

func validCreateInput() domain.CreateExternalApplicationInput {
	return domain.CreateExternalApplicationInput{
		FullName:         "Ivan Ivanov",
		Email:            "ivan@example.com",
		OrganisationName: "ACME",
		ProjectName:      "Project X",
		TypeID:           1,
		ExpectedResults:  "Useful outcomes",
		IsPayed:          true,
	}
}

func adminActor() domain.Actor {
	return domain.Actor{UserID: 1, Role: domain.RoleAdmin}
}

func userActor() domain.Actor {
	return domain.Actor{UserID: 2, Role: domain.RoleUser}
}

func TestServiceLogin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{
			user: domain.User{
				ID:           1,
				Login:        "admin",
				PasswordHash: hashedPassword(t, "admin123"),
				Role:         domain.RoleAdmin,
			},
		}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

		token, err := service.Login(context.Background(), "admin", "admin123")
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		if token == "" {
			t.Fatalf("Login() token is empty")
		}
	})

	t.Run("empty credentials", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

		_, err := service.Login(context.Background(), "", "")
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("Login() error = %v, want validation", err)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{err: domain.ErrNotFound}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

		_, err := service.Login(context.Background(), "missing", "secret")
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("Login() error = %v, want invalid credentials", err)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{
			user: domain.User{
				ID:           1,
				Login:        "admin",
				PasswordHash: hashedPassword(t, "admin123"),
				Role:         domain.RoleAdmin,
			},
		}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

		_, err := service.Login(context.Background(), "admin", "wrong")
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("Login() error = %v, want invalid credentials", err)
		}
	})
}

func TestServiceCreateExternalApplication(t *testing.T) {
	t.Run("success normalizes optional blank fields", func(t *testing.T) {
		repository := &fakeApplicationRepository{createID: 42}
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{exists: true}, repository)
		phone := "   "
		url := "   "
		additional := "   "
		input := validCreateInput()
		input.Phone = &phone
		input.OrganisationURL = &url
		input.AdditionalInformation = &additional

		id, err := service.CreateExternalApplication(context.Background(), input)
		if err != nil {
			t.Fatalf("CreateExternalApplication() error = %v", err)
		}
		if id != 42 {
			t.Fatalf("CreateExternalApplication() id = %d, want 42", id)
		}
		if repository.createInput.Phone != nil || repository.createInput.OrganisationURL != nil || repository.createInput.AdditionalInformation != nil {
			t.Fatalf("CreateExternalApplication() optional blanks were not normalized to nil: %#v", repository.createInput)
		}
	})

	t.Run("validation errors", func(t *testing.T) {
		longString := strings.Repeat("a", 151)
		longAdditional := strings.Repeat("a", 1501)
		testCases := []struct {
			name  string
			input domain.CreateExternalApplicationInput
		}{
			{name: "missing fullName", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				input.FullName = ""
				return input
			}()},
			{name: "fullName too long", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				input.FullName = longString
				return input
			}()},
			{name: "missing email", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				input.Email = ""
				return input
			}()},
			{name: "invalid email", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				input.Email = "not-an-email"
				return input
			}()},
			{name: "missing organisationName", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				input.OrganisationName = ""
				return input
			}()},
			{name: "missing projectName", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				input.ProjectName = ""
				return input
			}()},
			{name: "missing expectedResults", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				input.ExpectedResults = ""
				return input
			}()},
			{name: "invalid phone", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				phone := "call me at +7 (999) 123-45-67 please"
				input.Phone = &phone
				return input
			}()},
			{name: "invalid organisation url", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				value := "ftp://example.com"
				input.OrganisationURL = &value
				return input
			}()},
			{name: "additional info too long", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				value := longAdditional
				input.AdditionalInformation = &value
				return input
			}()},
			{name: "non-positive type id", input: func() domain.CreateExternalApplicationInput {
				input := validCreateInput()
				input.TypeID = 0
				return input
			}()},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{exists: true}, &fakeApplicationRepository{})

				_, err := service.CreateExternalApplication(context.Background(), tc.input)
				if !errors.Is(err, domain.ErrValidation) {
					t.Fatalf("CreateExternalApplication() error = %v, want validation", err)
				}
			})
		}
	})

	t.Run("unknown project type", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{exists: false}, &fakeApplicationRepository{})

		_, err := service.CreateExternalApplication(context.Background(), validCreateInput())
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("CreateExternalApplication() error = %v, want validation", err)
		}
	})
}

func TestServiceAcceptExternalApplication(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repository := &fakeApplicationRepository{
			application: domain.ExternalApplication{Status: domain.StatusPending},
		}
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, repository)

		if err := service.AcceptExternalApplication(context.Background(), adminActor(), 1); err != nil {
			t.Fatalf("AcceptExternalApplication() error = %v", err)
		}
		if repository.decision != domain.StatusAccepted {
			t.Fatalf("AcceptExternalApplication() status = %s, want ACCEPTED", repository.decision)
		}
		if repository.decisionReason != nil {
			t.Fatalf("AcceptExternalApplication() reason = %#v, want nil", repository.decisionReason)
		}
	})

	t.Run("invalid state", func(t *testing.T) {
		repository := &fakeApplicationRepository{
			application: domain.ExternalApplication{Status: domain.StatusRejected},
		}
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, repository)

		err := service.AcceptExternalApplication(context.Background(), adminActor(), 1)
		if !errors.Is(err, domain.ErrInvalidState) {
			t.Fatalf("AcceptExternalApplication() error = %v, want invalid state", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repository := &fakeApplicationRepository{getErr: domain.ErrNotFound}
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, repository)

		err := service.AcceptExternalApplication(context.Background(), adminActor(), 1)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("AcceptExternalApplication() error = %v, want not found", err)
		}
	})

	t.Run("requires admin actor", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

		if err := service.AcceptExternalApplication(context.Background(), domain.Actor{}, 1); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("AcceptExternalApplication() error = %v, want unauthorized", err)
		}
		if err := service.AcceptExternalApplication(context.Background(), userActor(), 1); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("AcceptExternalApplication() error = %v, want forbidden", err)
		}
	})
}

func TestServiceRejectExternalApplication(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repository := &fakeApplicationRepository{
			application: domain.ExternalApplication{Status: domain.StatusPending},
		}
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, repository)

		if err := service.RejectExternalApplication(context.Background(), adminActor(), 1, "out of scope"); err != nil {
			t.Fatalf("RejectExternalApplication() error = %v", err)
		}
		if repository.decision != domain.StatusRejected {
			t.Fatalf("RejectExternalApplication() status = %s, want REJECTED", repository.decision)
		}
		if repository.decisionReason == nil || *repository.decisionReason != "out of scope" {
			t.Fatalf("RejectExternalApplication() reason = %#v, want out of scope", repository.decisionReason)
		}
	})

	t.Run("requires reason", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

		err := service.RejectExternalApplication(context.Background(), adminActor(), 1, "   ")
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("RejectExternalApplication() error = %v, want validation", err)
		}
	})

	t.Run("reason too long", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

		err := service.RejectExternalApplication(context.Background(), adminActor(), 1, strings.Repeat("a", 751))
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("RejectExternalApplication() error = %v, want validation", err)
		}
	})

	t.Run("invalid state", func(t *testing.T) {
		repository := &fakeApplicationRepository{
			application: domain.ExternalApplication{Status: domain.StatusAccepted},
		}
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, repository)

		err := service.RejectExternalApplication(context.Background(), adminActor(), 1, "out of scope")
		if !errors.Is(err, domain.ErrInvalidState) {
			t.Fatalf("RejectExternalApplication() error = %v, want invalid state", err)
		}
	})

	t.Run("requires admin actor", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

		if err := service.RejectExternalApplication(context.Background(), domain.Actor{}, 1, "reason"); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("RejectExternalApplication() error = %v, want unauthorized", err)
		}
		if err := service.RejectExternalApplication(context.Background(), userActor(), 1, "reason"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("RejectExternalApplication() error = %v, want forbidden", err)
		}
	})
}

func TestServiceListExternalApplications(t *testing.T) {
	t.Run("applies defaults", func(t *testing.T) {
		repository := &fakeApplicationRepository{
			listResponse: domain.ExternalApplicationList{Count: 1},
		}
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{exists: true}, repository)

		_, err := service.ListExternalApplications(context.Background(), adminActor(), domain.ListExternalApplicationsFilter{})
		if err != nil {
			t.Fatalf("ListExternalApplications() error = %v", err)
		}
		if repository.listFilter.Limit != 20 {
			t.Fatalf("ListExternalApplications() limit = %d, want 20", repository.listFilter.Limit)
		}
		if repository.listFilter.SortByDateUpdated != domain.SortDesc {
			t.Fatalf("ListExternalApplications() sort = %s, want DESC", repository.listFilter.SortByDateUpdated)
		}
	})

	t.Run("invalid filters", func(t *testing.T) {
		projectTypeID := int64(99)
		testCases := []struct {
			name   string
			filter domain.ListExternalApplicationsFilter
		}{
			{
				name:   "negative offset",
				filter: domain.ListExternalApplicationsFilter{Offset: -1, Limit: 20},
			},
			{
				name:   "limit too high",
				filter: domain.ListExternalApplicationsFilter{Limit: 101},
			},
			{
				name:   "invalid sort",
				filter: domain.ListExternalApplicationsFilter{Limit: 20, SortByDateUpdated: "INVALID"},
			},
			{
				name: "unknown project type",
				filter: domain.ListExternalApplicationsFilter{
					Limit:         20,
					ProjectTypeID: &projectTypeID,
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				projectTypes := fakeProjectTypeRepository{exists: tc.name != "unknown project type"}
				service := newTestService(t, fakeUserRepository{}, projectTypes, &fakeApplicationRepository{})

				_, err := service.ListExternalApplications(context.Background(), adminActor(), tc.filter)
				if !errors.Is(err, domain.ErrValidation) {
					t.Fatalf("ListExternalApplications() error = %v, want validation", err)
				}
			})
		}
	})

	t.Run("requires admin actor", func(t *testing.T) {
		service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

		if _, err := service.ListExternalApplications(context.Background(), domain.Actor{}, domain.ListExternalApplicationsFilter{}); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("ListExternalApplications() error = %v, want unauthorized", err)
		}
		if _, err := service.ListExternalApplications(context.Background(), userActor(), domain.ListExternalApplicationsFilter{}); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("ListExternalApplications() error = %v, want forbidden", err)
		}
	})
}

func TestServiceGetExternalApplicationRequiresAdminActor(t *testing.T) {
	service := newTestService(t, fakeUserRepository{}, fakeProjectTypeRepository{}, &fakeApplicationRepository{})

	if _, err := service.GetExternalApplication(context.Background(), domain.Actor{}, 1); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("GetExternalApplication() error = %v, want unauthorized", err)
	}
	if _, err := service.GetExternalApplication(context.Background(), userActor(), 1); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("GetExternalApplication() error = %v, want forbidden", err)
	}
}
