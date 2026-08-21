package errors_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

func TestRequiredErrorCodes(t *testing.T) {
	tests := map[string]struct {
		code     apperrors.ErrorCode
		expected string
	}{
		"validation failed":    {apperrors.CodeValidationFailed, "VALIDATION_FAILED"},
		"invalid request":      {apperrors.CodeInvalidRequest, "INVALID_REQUEST"},
		"invalid credentials":  {apperrors.CodeInvalidCredentials, "INVALID_CREDENTIALS"},
		"session expired":      {apperrors.CodeSessionExpired, "SESSION_EXPIRED"},
		"session revoked":      {apperrors.CodeSessionRevoked, "SESSION_REVOKED"},
		"permission denied":    {apperrors.CodePermissionDenied, "PERMISSION_DENIED"},
		"tenant access denied": {apperrors.CodeTenantAccessDenied, "TENANT_ACCESS_DENIED"},
		"resource not found":   {apperrors.CodeResourceNotFound, "RESOURCE_NOT_FOUND"},
		"user not found":       {apperrors.CodeUserNotFound, "USER_NOT_FOUND"},
		"tenant not found":     {apperrors.CodeTenantNotFound, "TENANT_NOT_FOUND"},
		"user already exists":  {apperrors.CodeUserAlreadyExists, "USER_ALREADY_EXISTS"},
		"role already exists":  {apperrors.CodeRoleAlreadyExists, "ROLE_ALREADY_EXISTS"},
		"rate limited":         {apperrors.CodeRateLimited, "RATE_LIMITED"},
		"service unavailable":  {apperrors.CodeServiceUnavailable, "SERVICE_UNAVAILABLE"},
		"internal error":       {apperrors.CodeInternalError, "INTERNAL_ERROR"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if string(test.code) != test.expected {
				t.Fatalf("code = %q, want %q", test.code, test.expected)
			}
		})
	}
}

func TestAppErrorConstructionAndWrapping(t *testing.T) {
	cause := errors.New("database detail")
	appErr := apperrors.New(apperrors.CodeUserNotFound, "internal context", cause)

	if appErr.Code != apperrors.CodeUserNotFound {
		t.Fatalf("code = %q, want %q", appErr.Code, apperrors.CodeUserNotFound)
	}
	if appErr.Message != "internal context" {
		t.Fatalf("message = %q, want %q", appErr.Message, "internal context")
	}
	if !errors.Is(appErr, cause) {
		t.Fatal("application error does not unwrap its cause")
	}

	wrapped := fmt.Errorf("finding user: %w", appErr)
	var identified *apperrors.AppError
	if !errors.As(wrapped, &identified) {
		t.Fatal("errors.As did not identify the wrapped application error")
	}
	if identified.Code != apperrors.CodeUserNotFound {
		t.Fatalf("identified code = %q, want %q", identified.Code, apperrors.CodeUserNotFound)
	}
}

func TestErrorsAsFindsAppErrorThroughThreeWrappingLayers(t *testing.T) {
	appErr := apperrors.New(apperrors.CodeTenantNotFound, "internal context", nil)
	wrapped := fmt.Errorf("repository context: %w", appErr)
	wrapped = fmt.Errorf("service context: %w", wrapped)
	wrapped = fmt.Errorf("handler context: %w", wrapped)

	var identified *apperrors.AppError
	if !errors.As(wrapped, &identified) {
		t.Fatal("errors.As did not identify the application error through three wrapping layers")
	}
	if identified.Code != apperrors.CodeTenantNotFound {
		t.Fatalf("identified code = %q, want %q", identified.Code, apperrors.CodeTenantNotFound)
	}
}

func TestMapKnownErrorsUsesSafePublicMessages(t *testing.T) {
	appErr := apperrors.New(apperrors.CodeUserNotFound, "user secret and tenant identifier", errors.New("SQL: SELECT * FROM users"))
	result := apperrors.Map(fmt.Errorf("service context: %w", appErr))

	if result.Status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusNotFound)
	}
	if result.Code != apperrors.CodeUserNotFound {
		t.Fatalf("code = %q, want %q", result.Code, apperrors.CodeUserNotFound)
	}
	if strings.Contains(result.Message, "secret") || strings.Contains(result.Message, "tenant") {
		t.Fatalf("public message leaked caller context: %q", result.Message)
	}
	if result.Message == "user secret and tenant identifier" {
		t.Fatal("public message used the arbitrary application error message")
	}
}

func TestHTTPStatusMapping(t *testing.T) {
	tests := map[string]struct {
		code   apperrors.ErrorCode
		status int
	}{
		"validation failed":    {apperrors.CodeValidationFailed, http.StatusBadRequest},
		"invalid request":      {apperrors.CodeInvalidRequest, http.StatusBadRequest},
		"invalid credentials":  {apperrors.CodeInvalidCredentials, http.StatusUnauthorized},
		"session expired":      {apperrors.CodeSessionExpired, http.StatusUnauthorized},
		"session revoked":      {apperrors.CodeSessionRevoked, http.StatusUnauthorized},
		"permission denied":    {apperrors.CodePermissionDenied, http.StatusForbidden},
		"tenant access denied": {apperrors.CodeTenantAccessDenied, http.StatusForbidden},
		"resource not found":   {apperrors.CodeResourceNotFound, http.StatusNotFound},
		"user not found":       {apperrors.CodeUserNotFound, http.StatusNotFound},
		"tenant not found":     {apperrors.CodeTenantNotFound, http.StatusNotFound},
		"user already exists":  {apperrors.CodeUserAlreadyExists, http.StatusConflict},
		"role already exists":  {apperrors.CodeRoleAlreadyExists, http.StatusConflict},
		"rate limited":         {apperrors.CodeRateLimited, http.StatusTooManyRequests},
		"internal error":       {apperrors.CodeInternalError, http.StatusInternalServerError},
		"service unavailable":  {apperrors.CodeServiceUnavailable, http.StatusServiceUnavailable},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			result := apperrors.Map(apperrors.New(test.code, "ignored public context", nil))
			if result.Status != test.status {
				t.Fatalf("status = %d, want %d", result.Status, test.status)
			}
			if result.Code != test.code {
				t.Fatalf("code = %q, want %q", result.Code, test.code)
			}
		})
	}
}

func TestMapUnexpectedErrorUsesInternalError(t *testing.T) {
	result := apperrors.Map(errors.New("SQL password=super-secret at C:\\private\\db"))

	if result.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusInternalServerError)
	}
	if result.Code != apperrors.CodeInternalError {
		t.Fatalf("code = %q, want %q", result.Code, apperrors.CodeInternalError)
	}
	if strings.Contains(result.Message, "super-secret") || strings.Contains(result.Message, "private") {
		t.Fatalf("unexpected error leaked: %q", result.Message)
	}
}

func TestMapNilUsesInternalErrorFallback(t *testing.T) {
	result := apperrors.Map(nil)

	if result.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusInternalServerError)
	}
	if result.Code != apperrors.CodeInternalError {
		t.Fatalf("code = %q, want %q", result.Code, apperrors.CodeInternalError)
	}
	if result.Message != "An unexpected error occurred." {
		t.Fatalf("message = %q, want centralized generic safe message", result.Message)
	}
}

func TestMapServiceUnavailable(t *testing.T) {
	result := apperrors.Map(apperrors.New(apperrors.CodeServiceUnavailable, "dependency details", errors.New("connection refused")))

	if result.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusServiceUnavailable)
	}
	if result.Code != apperrors.CodeServiceUnavailable {
		t.Fatalf("code = %q, want %q", result.Code, apperrors.CodeServiceUnavailable)
	}
	if result.Message != "The service is temporarily unavailable." {
		t.Fatalf("message = %q, want safe service-unavailable message", result.Message)
	}
}

func TestMapUnknownCodeUsesInternalError(t *testing.T) {
	result := apperrors.Map(apperrors.New(apperrors.ErrorCode("FUTURE_CODE"), "future details", nil))

	if result.Status != http.StatusInternalServerError || result.Code != apperrors.CodeInternalError {
		t.Fatalf("result = %#v, want internal error/500", result)
	}
}

func TestWriteJSONSerializesOnlyPublicFields(t *testing.T) {
	response := apperrors.Map(apperrors.New(apperrors.CodeInvalidCredentials, "password and SQL details", errors.New("secret")))
	recorder := &responseRecorder{}

	if err := response.WriteJSON(recorder); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	if recorder.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.status, http.StatusUnauthorized)
	}
	var body map[string]map[string]string
	if err := json.Unmarshal(recorder.body, &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["error"]["code"] != string(apperrors.CodeInvalidCredentials) {
		t.Fatalf("serialized body = %s", recorder.body)
	}
	if strings.Contains(string(recorder.body), "password") || strings.Contains(string(recorder.body), "secret") {
		t.Fatalf("serialized response leaked internal details: %s", recorder.body)
	}
}

type responseRecorder struct {
	header http.Header
	status int
	body   []byte
}

func (r *responseRecorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *responseRecorder) Write(body []byte) (int, error) {
	r.body = append(r.body, body...)
	return len(body), nil
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
}
