package transporthttp

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"Projects_Service/internal/domain"
	"Projects_Service/internal/platform/auth"
)

type fakeService struct {
	createFn func(ctx context.Context, input domain.CreateExternalApplicationInput) (int64, error)
}

func (f fakeService) Login(context.Context, string, string) (string, error) {
	return "", nil
}

func (f fakeService) ListProjectTypes(context.Context) ([]domain.ProjectType, error) {
	return nil, nil
}

func (f fakeService) CreateExternalApplication(ctx context.Context, input domain.CreateExternalApplicationInput) (int64, error) {
	if f.createFn != nil {
		return f.createFn(ctx, input)
	}
	return 1, nil
}

func (f fakeService) GetExternalApplication(context.Context, domain.Actor, int64) (domain.ExternalApplication, error) {
	return domain.ExternalApplication{}, nil
}

func (f fakeService) AcceptExternalApplication(context.Context, domain.Actor, int64) error {
	return nil
}

func (f fakeService) RejectExternalApplication(context.Context, domain.Actor, int64, string) error {
	return nil
}

func (f fakeService) ListExternalApplications(context.Context, domain.Actor, domain.ListExternalApplicationsFilter) (domain.ExternalApplicationList, error) {
	return domain.ExternalApplicationList{}, nil
}

type fakeUserReader struct{}

func (fakeUserReader) GetByID(context.Context, int64) (domain.User, error) {
	return domain.User{ID: 1, Role: domain.RoleAdmin}, nil
}

func TestCreateExternalApplicationRejectsTooLargeBody(t *testing.T) {
	handler := NewHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		fakeService{},
		fakeUserReader{},
		auth.NewTokenManager("secret"),
	)

	payload := append([]byte(`{"fullName":"`), bytes.Repeat([]byte("a"), int(maxRequestBodyBytes))...)
	payload = append(payload, []byte(`","email":"ivan@example.com","organisationName":"ACME","projectName":"Project X","typeId":1,"expectedResults":"Results","isPayed":true}`)...)
	request := httptest.NewRequest(http.MethodPost, "/project/application/external", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestAdminRouteRequiresActorInjectedByMiddleware(t *testing.T) {
	handler := &Handler{
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		service:      fakeService{},
		users:        fakeUserReader{},
		tokenManager: auth.NewTokenManager("secret"),
	}

	request := httptest.NewRequest(http.MethodGet, "/project/application/external/1", nil)
	request.SetPathValue("applicationId", "1")
	response := httptest.NewRecorder()

	handler.handleGetExternalApplication(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
