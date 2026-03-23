package transporthttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"Projects_Service/internal/domain"
	"Projects_Service/internal/platform/auth"
)

type service interface {
	Login(ctx context.Context, login, password string) (string, error)
	ListProjectTypes(ctx context.Context) ([]domain.ProjectType, error)
	CreateExternalApplication(ctx context.Context, input domain.CreateExternalApplicationInput) (int64, error)
	GetExternalApplication(ctx context.Context, actor domain.Actor, id int64) (domain.ExternalApplication, error)
	AcceptExternalApplication(ctx context.Context, actor domain.Actor, id int64) error
	RejectExternalApplication(ctx context.Context, actor domain.Actor, id int64, reason string) error
	ListExternalApplications(ctx context.Context, actor domain.Actor, filter domain.ListExternalApplicationsFilter) (domain.ExternalApplicationList, error)
}

type userReader interface {
	GetByID(ctx context.Context, id int64) (domain.User, error)
}

type Handler struct {
	logger       *slog.Logger
	service      service
	users        userReader
	tokenManager *auth.TokenManager
}

type contextKey string

const requestMetaKey contextKey = "request_meta"
const actorKey contextKey = "actor"

const maxRequestBodyBytes int64 = 1 << 20

var errRequestBodyTooLarge = errors.New("request body too large")

type requestMeta struct {
	requestID     string
	userID        int64
	errorCategory string
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func NewHandler(logger *slog.Logger, service service, users userReader, tokenManager *auth.TokenManager) http.Handler {
	h := &Handler{
		logger:       logger,
		service:      service,
		users:        users,
		tokenManager: tokenManager,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", h.handleLogin)
	mux.HandleFunc("GET /project/type", h.handleListProjectTypes)
	mux.HandleFunc("POST /project/application/external", h.handleCreateExternalApplication)
	mux.Handle("GET /project/application/external/list", h.withAdmin(http.HandlerFunc(h.handleListExternalApplications)))
	mux.Handle("GET /project/application/external/{applicationId}", h.withAdmin(http.HandlerFunc(h.handleGetExternalApplication)))
	mux.Handle("POST /project/application/external/{applicationId}/accept", h.withAdmin(http.HandlerFunc(h.handleAcceptExternalApplication)))
	mux.Handle("POST /project/application/external/{applicationId}/reject", h.withAdmin(http.HandlerFunc(h.handleRejectExternalApplication)))

	return h.withMiddleware(mux)
}

func (h *Handler) withMiddleware(next http.Handler) http.Handler {
	return h.requestID(h.logging(h.recoverer(next)))
}

func (h *Handler) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := metaFromContext(r.Context())
		start := time.Now()
		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		args := []any{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int("response_bytes", recorder.size),
			slog.Duration("duration", time.Since(start)),
		}
		if meta != nil && meta.requestID != "" {
			args = append(args, slog.String("request_id", meta.requestID))
		}
		if meta != nil && meta.userID > 0 {
			args = append(args, slog.Int64("user_id", meta.userID))
		}
		if meta != nil && meta.errorCategory != "" {
			args = append(args, slog.String("error_category", meta.errorCategory))
		}

		h.logger.Info("request handled", args...)
	})
}

func (h *Handler) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				setErrorCategory(r.Context(), "internal")
				h.logger.Error("panic recovered", slog.Any("panic", recovered))
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID()
		}

		meta := &requestMeta{requestID: requestID}
		ctx := context.WithValue(r.Context(), requestMetaKey, meta)
		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) withAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.Header.Get("X-API-TOKEN"))
		if token == "" {
			writeMappedError(r.Context(), w, domain.ErrUnauthorized)
			return
		}

		claims, err := h.tokenManager.Parse(token)
		if err != nil {
			writeMappedError(r.Context(), w, domain.ErrUnauthorized)
			return
		}
		if claims.UserID <= 0 {
			writeMappedError(r.Context(), w, domain.ErrUnauthorized)
			return
		}

		user, err := h.users.GetByID(r.Context(), claims.UserID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeMappedError(r.Context(), w, domain.ErrUnauthorized)
				return
			}
			writeMappedError(r.Context(), w, err)
			return
		}
		if user.Role != domain.RoleAdmin {
			writeMappedError(r.Context(), w, domain.ErrForbidden)
			return
		}

		setUserID(r.Context(), user.ID)
		actor := domain.Actor{
			UserID: user.ID,
			Role:   user.Role,
		}
		ctx := context.WithValue(r.Context(), actorKey, actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}

	if err := decodeJSON(w, r, &request); err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	token, err := h.service.Login(r.Context(), request.Login, request.Password)
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (h *Handler) handleListProjectTypes(w http.ResponseWriter, r *http.Request) {
	projectTypes, err := h.service.ListProjectTypes(r.Context())
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, projectTypes)
}

func (h *Handler) handleCreateExternalApplication(w http.ResponseWriter, r *http.Request) {
	var request domain.CreateExternalApplicationInput
	if err := decodeJSON(w, r, &request); err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	id, err := h.service.CreateExternalApplication(r.Context(), request)
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, id)
}

func (h *Handler) handleGetExternalApplication(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "applicationId")
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	actor, err := actorFromContext(r.Context())
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	applicationModel, err := h.service.GetExternalApplication(r.Context(), actor, id)
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	response := struct {
		ApplicationID         int64                            `json:"applicationId"`
		Initiator             string                           `json:"initiator"`
		Email                 string                           `json:"email"`
		Phone                 *string                          `json:"phone"`
		OrganisationName      string                           `json:"organisationName"`
		OrganisationURL       *string                          `json:"organisationUrl"`
		ProjectName           string                           `json:"projectName"`
		TypeName              string                           `json:"typeName"`
		ExpectedResults       string                           `json:"expectedResults"`
		IsPayed               bool                             `json:"isPayed"`
		AdditionalInformation *string                          `json:"additionalInformation"`
		Status                domain.ExternalApplicationStatus `json:"status"`
	}{
		ApplicationID:         applicationModel.ID,
		Initiator:             applicationModel.FullName,
		Email:                 applicationModel.Email,
		Phone:                 applicationModel.Phone,
		OrganisationName:      applicationModel.OrganisationName,
		OrganisationURL:       applicationModel.OrganisationURL,
		ProjectName:           applicationModel.ProjectName,
		TypeName:              applicationModel.TypeName,
		ExpectedResults:       applicationModel.ExpectedResults,
		IsPayed:               applicationModel.IsPayed,
		AdditionalInformation: applicationModel.AdditionalInformation,
		Status:                applicationModel.Status,
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleAcceptExternalApplication(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "applicationId")
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	actor, err := actorFromContext(r.Context())
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	if err := h.service.AcceptExternalApplication(r.Context(), actor, id); err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleRejectExternalApplication(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "applicationId")
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	var request struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	actor, err := actorFromContext(r.Context())
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	if err := h.service.RejectExternalApplication(r.Context(), actor, id, request.Reason); err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleListExternalApplications(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	active := true
	if query.Has("active") {
		parsed, err := strconv.ParseBool(query.Get("active"))
		if err != nil {
			writeMappedError(r.Context(), w, domain.ValidationError{Message: "active must be a boolean"})
			return
		}
		active = parsed
	}

	limit := 20
	if query.Get("limit") != "" {
		value, err := strconv.Atoi(query.Get("limit"))
		if err != nil {
			writeMappedError(r.Context(), w, domain.ValidationError{Message: "limit must be an integer"})
			return
		}
		limit = value
	}

	offset := 0
	if query.Get("offset") != "" {
		value, err := strconv.Atoi(query.Get("offset"))
		if err != nil {
			writeMappedError(r.Context(), w, domain.ValidationError{Message: "offset must be an integer"})
			return
		}
		offset = value
	}

	filter := domain.ListExternalApplicationsFilter{
		ActiveOnly:        &active,
		Search:            query.Get("search"),
		SortByDateUpdated: domain.SortType(query.Get("sortByDateUpdated")),
		Limit:             limit,
		Offset:            offset,
	}

	if query.Get("projectTypeId") != "" {
		projectTypeID, err := strconv.ParseInt(query.Get("projectTypeId"), 10, 64)
		if err != nil {
			writeMappedError(r.Context(), w, domain.ValidationError{Message: "projectTypeId must be an integer"})
			return
		}
		filter.ProjectTypeID = &projectTypeID
	}

	actor, err := actorFromContext(r.Context())
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	response, err := h.service.ListExternalApplications(r.Context(), actor, filter)
	if err != nil {
		writeMappedError(r.Context(), w, err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return errRequestBodyTooLarge
		}
		return domain.ValidationError{Message: "invalid json body"}
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return domain.ValidationError{Message: "invalid json body"}
	} else if isMaxBytesError(err) {
		return errRequestBodyTooLarge
	} else if !errors.Is(err, io.EOF) {
		return domain.ValidationError{Message: "invalid json body"}
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeMappedError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRequestBodyTooLarge):
		setErrorCategory(ctx, "validation")
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
	case errors.Is(err, domain.ErrUnauthorized):
		setErrorCategory(ctx, "unauthorized")
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, domain.ErrForbidden):
		setErrorCategory(ctx, "forbidden")
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, domain.ErrNotFound):
		setErrorCategory(ctx, "not_found")
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrValidation):
		setErrorCategory(ctx, "validation")
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrInvalidCredentials):
		setErrorCategory(ctx, "unauthorized")
		writeError(w, http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, domain.ErrInvalidState):
		setErrorCategory(ctx, "validation")
		writeError(w, http.StatusBadRequest, "application is not in PENDING status")
	default:
		setErrorCategory(ctx, "internal")
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func parsePathInt64(r *http.Request, key string) (int64, error) {
	value := r.PathValue(key)
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, domain.ValidationError{Message: key + " must be a positive integer"}
	}

	return id, nil
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}

	size, err := r.ResponseWriter.Write(data)
	r.size += size
	return size, err
}

func metaFromContext(ctx context.Context) *requestMeta {
	meta, _ := ctx.Value(requestMetaKey).(*requestMeta)
	return meta
}

func setUserID(ctx context.Context, userID int64) {
	if meta := metaFromContext(ctx); meta != nil {
		meta.userID = userID
	}
}

func setErrorCategory(ctx context.Context, category string) {
	if meta := metaFromContext(ctx); meta != nil {
		meta.errorCategory = category
	}
}

func newRequestID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}

	return strconv.FormatInt(time.Now().UnixNano(), 16)
}

func actorFromContext(ctx context.Context) (domain.Actor, error) {
	actor, ok := ctx.Value(actorKey).(domain.Actor)
	if !ok {
		return domain.Actor{}, domain.ErrUnauthorized
	}

	return actor, nil
}

func isMaxBytesError(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}
