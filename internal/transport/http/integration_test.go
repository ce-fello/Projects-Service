package transporthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"Projects_Service/internal/application"
	"Projects_Service/internal/config"
	"Projects_Service/internal/platform/auth"
	"Projects_Service/internal/platform/postgres"
	transporthttp "Projects_Service/internal/transport/http"
)

func TestIntegrationApplicationLifecycleAndAuth(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	applicationID := createApplication(t, ts.URL, map[string]any{
		"fullName":              "Ivan Ivanov",
		"email":                 "ivan@example.com",
		"phone":                 "+7 (999) 123-45-67",
		"organisationName":      "ACME",
		"organisationUrl":       "https://acme.test",
		"projectName":           "New Venture",
		"typeId":                1,
		"expectedResults":       "Launch MVP",
		"isPayed":               true,
		"additionalInformation": "Important details",
	})

	assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/"+applicationID, "", ""), http.StatusUnauthorized)
	assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/"+applicationID, "invalid-token", ""), http.StatusUnauthorized)

	userToken := login(t, ts.URL, "user", "user123")
	assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/"+applicationID, userToken, ""), http.StatusForbidden)

	adminToken := login(t, ts.URL, "admin", "admin123")

	assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/not-an-id", adminToken, ""), http.StatusBadRequest)
	assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/99999", adminToken, ""), http.StatusNotFound)

	detailResp := doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/"+applicationID, adminToken, "")
	assertStatus(t, detailResp, http.StatusOK)

	var detail struct {
		ApplicationID         int64   `json:"applicationId"`
		Initiator             string  `json:"initiator"`
		Email                 string  `json:"email"`
		Phone                 *string `json:"phone"`
		OrganisationName      string  `json:"organisationName"`
		OrganisationURL       *string `json:"organisationUrl"`
		ProjectName           string  `json:"projectName"`
		TypeName              string  `json:"typeName"`
		ExpectedResults       string  `json:"expectedResults"`
		AdditionalInformation *string `json:"additionalInformation"`
		Status                string  `json:"status"`
	}
	decodeResponse(t, detailResp, &detail)
	if detail.ApplicationID == 0 || detail.Status != "PENDING" || detail.TypeName != "Research" {
		t.Fatalf("unexpected detail payload: %#v", detail)
	}
	if detail.Phone == nil || *detail.Phone != "+7 (999) 123-45-67" {
		t.Fatalf("detail phone = %#v, want populated phone", detail.Phone)
	}

	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/"+applicationID+"/accept", adminToken, ""), http.StatusOK)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/"+applicationID+"/accept", adminToken, ""), http.StatusBadRequest)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/"+applicationID+"/reject", adminToken, `{"reason":"late reject"}`), http.StatusBadRequest)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/99999/accept", adminToken, ""), http.StatusNotFound)
}

func TestIntegrationAdminAccessRevalidatesUserFromDatabase(t *testing.T) {
	t.Run("role change revokes admin access", func(t *testing.T) {
		ts := newTestServer(t)
		defer ts.Close()

		adminToken := login(t, ts.URL, "admin", "admin123")
		db := openSharedTestDB(t)

		if _, err := db.Exec(context.Background(), `UPDATE users SET role = 'USER' WHERE login = 'admin'`); err != nil {
			t.Fatalf("update role: %v", err)
		}

		assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list", adminToken, ""), http.StatusForbidden)
	})

	t.Run("deleted user loses access", func(t *testing.T) {
		ts := newTestServer(t)
		defer ts.Close()

		adminToken := login(t, ts.URL, "admin", "admin123")
		db := openSharedTestDB(t)

		if _, err := db.Exec(context.Background(), `DELETE FROM users WHERE login = 'admin'`); err != nil {
			t.Fatalf("delete user: %v", err)
		}

		assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list", adminToken, ""), http.StatusUnauthorized)
	})
}

func TestIntegrationLoginProjectTypesAndListAuth(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/login", "", `{"login":"admin","password":"admin123"}`), http.StatusOK)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/login", "", `{"login":"admin","password":"wrong"}`), http.StatusUnauthorized)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/login", "", `{"login":"admin","password":"admin123","unexpected":"value"}`), http.StatusBadRequest)

	projectTypesResp := doJSONRequest(t, http.MethodGet, ts.URL+"/project/type", "", "")
	assertStatus(t, projectTypesResp, http.StatusOK)
	var projectTypes []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	decodeResponse(t, projectTypesResp, &projectTypes)
	if len(projectTypes) < 2 {
		t.Fatalf("project types len = %d, want at least 2", len(projectTypes))
	}

	assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list", "", ""), http.StatusUnauthorized)
	assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list", "invalid-token", ""), http.StatusUnauthorized)

	userToken := login(t, ts.URL, "user", "user123")
	assertStatus(t, doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list", userToken, ""), http.StatusForbidden)

	requestIDResp := doJSONRequestWithHeaders(t, http.MethodGet, ts.URL+"/project/type", "", "", map[string]string{"X-Request-ID": "custom-request-id"})
	assertStatus(t, requestIDResp, http.StatusOK)
	if requestIDResp.Header.Get("X-Request-ID") != "custom-request-id" {
		t.Fatalf("X-Request-ID = %q, want custom-request-id", requestIDResp.Header.Get("X-Request-ID"))
	}

	generatedRequestIDResp := doJSONRequest(t, http.MethodGet, ts.URL+"/project/type", "", "")
	assertStatus(t, generatedRequestIDResp, http.StatusOK)
	if generatedRequestIDResp.Header.Get("X-Request-ID") == "" {
		t.Fatalf("generated X-Request-ID is empty")
	}
}

func TestIntegrationCreateExternalApplicationValidation(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	testCases := []struct {
		name   string
		body   string
		status int
	}{
		{
			name:   "rejectedReason is forbidden",
			body:   `{"fullName":"Ivan Ivanov","email":"ivan@example.com","organisationName":"ACME","projectName":"Project X","typeId":1,"expectedResults":"Results","isPayed":true,"rejectedReason":"forbidden"}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "unknown project type",
			body:   `{"fullName":"Ivan Ivanov","email":"ivan@example.com","organisationName":"ACME","projectName":"Project X","typeId":999,"expectedResults":"Results","isPayed":true}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "missing required field",
			body:   `{"fullName":"Ivan Ivanov","organisationName":"ACME","projectName":"Project X","typeId":1,"expectedResults":"Results","isPayed":true}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "invalid phone",
			body:   `{"fullName":"Ivan Ivanov","email":"ivan@example.com","phone":"call me at +7 (999) 123-45-67 please","organisationName":"ACME","projectName":"Project X","typeId":1,"expectedResults":"Results","isPayed":true}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "invalid email",
			body:   `{"fullName":"Ivan Ivanov","email":"not-an-email","organisationName":"ACME","projectName":"Project X","typeId":1,"expectedResults":"Results","isPayed":true}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "invalid organisation url",
			body:   `{"fullName":"Ivan Ivanov","email":"ivan@example.com","organisationName":"ACME","organisationUrl":"ftp://example.com","projectName":"Project X","typeId":1,"expectedResults":"Results","isPayed":true}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "unknown field",
			body:   `{"fullName":"Ivan Ivanov","email":"ivan@example.com","organisationName":"ACME","projectName":"Project X","typeId":1,"expectedResults":"Results","isPayed":true,"unexpected":"value"}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "malformed json",
			body:   `{"fullName":"Ivan Ivanov"`,
			status: http.StatusBadRequest,
		},
		{
			name:   "multiple objects",
			body:   `{"fullName":"Ivan Ivanov","email":"ivan@example.com","organisationName":"ACME","projectName":"Project X","typeId":1,"expectedResults":"Results","isPayed":true}{"second":true}`,
			status: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external", "", tc.body), tc.status)
		})
	}
}

func TestIntegrationRejectValidationAndNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	adminToken := login(t, ts.URL, "admin", "admin123")
	applicationID := createApplication(t, ts.URL, map[string]any{
		"fullName":         "Ivan Ivanov",
		"email":            "ivan@example.com",
		"organisationName": "ACME",
		"projectName":      "Project X",
		"typeId":           1,
		"expectedResults":  "Results",
		"isPayed":          true,
	})

	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/"+applicationID+"/reject", adminToken, `{"reason":"   "}`), http.StatusBadRequest)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/"+applicationID+"/reject", adminToken, `{}`), http.StatusBadRequest)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/"+applicationID+"/reject", adminToken, `{"reason":"ok","unexpected":"value"}`), http.StatusBadRequest)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/not-an-id/reject", adminToken, `{"reason":"ok"}`), http.StatusBadRequest)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/99999/reject", adminToken, `{"reason":"ok"}`), http.StatusNotFound)
}

func TestIntegrationListFiltersAndValidation(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	adminToken := login(t, ts.URL, "admin", "admin123")

	pendingID := createApplication(t, ts.URL, map[string]any{
		"fullName":         "Ivan Ivanov",
		"email":            "ivan@example.com",
		"organisationName": "ACME",
		"projectName":      "Alpha Research",
		"typeId":           1,
		"expectedResults":  "Report",
		"isPayed":          true,
	})
	acceptedID := createApplication(t, ts.URL, map[string]any{
		"fullName":         "Petr Petrov",
		"email":            "petr@example.com",
		"organisationName": "Beta LLC",
		"projectName":      "Beta Build",
		"typeId":           2,
		"expectedResults":  "Prototype",
		"isPayed":          false,
	})
	rejectedID := createApplication(t, ts.URL, map[string]any{
		"fullName":         "Sergey Sergeev",
		"email":            "sergey@example.com",
		"organisationName": "Gamma LLC",
		"projectName":      "Gamma Build",
		"typeId":           2,
		"expectedResults":  "Prototype",
		"isPayed":          false,
	})

	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/"+acceptedID+"/accept", adminToken, ""), http.StatusOK)
	assertStatus(t, doJSONRequest(t, http.MethodPost, ts.URL+"/project/application/external/"+rejectedID+"/reject", adminToken, `{"reason":"not aligned"}`), http.StatusOK)

	activeResp := doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list?active=true", adminToken, "")
	assertStatus(t, activeResp, http.StatusOK)
	var activeList struct {
		Count        int `json:"count"`
		Applications []struct {
			ExternalApplicationID int64  `json:"externalApplicationId"`
			Status                string `json:"status"`
		} `json:"applications"`
	}
	decodeResponse(t, activeResp, &activeList)
	if activeList.Count != 1 || len(activeList.Applications) != 1 || strconv.FormatInt(activeList.Applications[0].ExternalApplicationID, 10) != pendingID || activeList.Applications[0].Status != "PENDING" {
		t.Fatalf("unexpected active list payload: %#v", activeList)
	}

	processedResp := doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list?active=false&projectTypeId=2&sortByDateUpdated=DESC", adminToken, "")
	assertStatus(t, processedResp, http.StatusOK)
	var processedList struct {
		Count        int `json:"count"`
		Applications []struct {
			ExternalApplicationID int64   `json:"externalApplicationId"`
			Status                string  `json:"status"`
			RejectionMessage      *string `json:"rejectionMessage"`
		} `json:"applications"`
	}
	decodeResponse(t, processedResp, &processedList)
	if processedList.Count != 2 || len(processedList.Applications) != 2 {
		t.Fatalf("unexpected processed list payload: %#v", processedList)
	}

	searchByProjectResp := doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list?active=false&search=gamma", adminToken, "")
	assertStatus(t, searchByProjectResp, http.StatusOK)
	var searchByProject struct {
		Count int `json:"count"`
	}
	decodeResponse(t, searchByProjectResp, &searchByProject)
	if searchByProject.Count != 1 {
		t.Fatalf("search by project count = %d, want 1", searchByProject.Count)
	}

	searchByInitiatorResp := doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list?active=false&search=sergey", adminToken, "")
	assertStatus(t, searchByInitiatorResp, http.StatusOK)
	var searchByInitiator struct {
		Count int `json:"count"`
	}
	decodeResponse(t, searchByInitiatorResp, &searchByInitiator)
	if searchByInitiator.Count != 1 {
		t.Fatalf("search by initiator count = %d, want 1", searchByInitiator.Count)
	}

	paginationResp := doJSONRequest(t, http.MethodGet, ts.URL+"/project/application/external/list?active=false&limit=1&offset=1", adminToken, "")
	assertStatus(t, paginationResp, http.StatusOK)
	var paginationList struct {
		Count        int        `json:"count"`
		Applications []struct{} `json:"applications"`
	}
	decodeResponse(t, paginationResp, &paginationList)
	if paginationList.Count != 2 || len(paginationList.Applications) != 1 {
		t.Fatalf("unexpected pagination payload: %#v", paginationList)
	}

	validationCases := []string{
		ts.URL + "/project/application/external/list?active=invalid",
		ts.URL + "/project/application/external/list?limit=invalid",
		ts.URL + "/project/application/external/list?offset=invalid",
		ts.URL + "/project/application/external/list?projectTypeId=invalid",
		ts.URL + "/project/application/external/list?sortByDateUpdated=INVALID",
		ts.URL + "/project/application/external/list?projectTypeId=999",
		ts.URL + "/project/application/external/list?offset=-1",
		ts.URL + "/project/application/external/list?limit=101",
	}

	for _, url := range validationCases {
		assertStatus(t, doJSONRequest(t, http.MethodGet, url, adminToken, ""), http.StatusBadRequest)
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	cfg := config.Config{
		HTTPPort:    "8000",
		DatabaseURL: databaseURL,
		JWTSecret:   "integration-secret",
	}

	db, err := postgres.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := postgres.RunMigrations(context.Background(), db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := resetDatabase(context.Background(), db); err != nil {
		t.Fatalf("reset db: %v", err)
	}
	if err := postgres.Seed(context.Background(), db); err != nil {
		t.Fatalf("seed db: %v", err)
	}

	tokenManager := auth.NewTokenManager(cfg.JWTSecret)
	service := application.NewService(
		postgres.NewUserRepository(db),
		postgres.NewProjectTypeRepository(db),
		postgres.NewExternalApplicationRepository(db),
		postgres.NewTransactor(db),
		tokenManager,
	)

	handler := transporthttp.NewHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		service,
		postgres.NewUserRepository(db),
		tokenManager,
	)

	return httptest.NewServer(handler)
}

func resetDatabase(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `
		TRUNCATE TABLE external_applications, users, project_types RESTART IDENTITY CASCADE
	`)
	return err
}

func login(t *testing.T, baseURL, login, password string) string {
	t.Helper()

	response := doJSONRequest(t, http.MethodPost, baseURL+"/login", "", `{"login":"`+login+`","password":"`+password+`"}`)
	assertStatus(t, response, http.StatusOK)

	var payload struct {
		Token string `json:"token"`
	}
	decodeResponse(t, response, &payload)
	if payload.Token == "" {
		t.Fatalf("login token is empty")
	}

	return payload.Token
}

func createApplication(t *testing.T, baseURL string, payload map[string]any) string {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	response := doJSONRequest(t, http.MethodPost, baseURL+"/project/application/external", "", string(body))
	assertStatus(t, response, http.StatusOK)

	var applicationID int64
	decodeResponse(t, response, &applicationID)
	return strconv.FormatInt(applicationID, 10)
}

func doJSONRequest(t *testing.T, method, url, token, body string) *http.Response {
	t.Helper()

	return doJSONRequestWithHeaders(t, method, url, token, body, nil)
}

func doJSONRequestWithHeaders(t *testing.T, method, url, token, body string, headers map[string]string) *http.Response {
	t.Helper()

	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("X-API-TOKEN", token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })

	return response
}

func openSharedTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	db, err := postgres.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open shared db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

func decodeResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()

	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func assertStatus(t *testing.T, response *http.Response, expected int) {
	t.Helper()

	if response.StatusCode != expected {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want %d, body = %s", response.StatusCode, expected, string(body))
	}
}
