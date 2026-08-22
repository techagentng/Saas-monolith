package errors

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
)

// HTTPError is the sanitized transport representation of an application error.
type HTTPError struct {
	Status  int
	Code    ErrorCode
	Message string
}

// Map converts an error into a safe HTTP representation.
func Map(err error) HTTPError {
	var appErr *AppError
	if !stderrors.As(err, &appErr) || !isKnownCode(appErr.Code) {
		return internalHTTPError()
	}

	return HTTPError{
		Status:  statusFor(appErr.Code),
		Code:    appErr.Code,
		Message: messageFor(appErr.Code),
	}
}

func (e HTTPError) WriteJSON(writer http.ResponseWriter) error {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(e.Status)
	return json.NewEncoder(writer).Encode(struct {
		Error struct {
			Code    ErrorCode `json:"code"`
			Message string    `json:"message"`
		} `json:"error"`
	}{Error: struct {
		Code    ErrorCode `json:"code"`
		Message string    `json:"message"`
	}{Code: e.Code, Message: e.Message}})
}

func internalHTTPError() HTTPError {
	return HTTPError{
		Status:  http.StatusInternalServerError,
		Code:    CodeInternalError,
		Message: "An unexpected error occurred.",
	}
}

func isKnownCode(code ErrorCode) bool {
	_, ok := statusByCode[code]
	return ok
}

func statusFor(code ErrorCode) int {
	return statusByCode[code]
}

var statusByCode = map[ErrorCode]int{
	CodeValidationFailed:              http.StatusBadRequest,
	CodeInvalidRequest:                http.StatusBadRequest,
	CodeInvalidCredentials:            http.StatusUnauthorized,
	CodeSessionExpired:                http.StatusUnauthorized,
	CodeSessionRevoked:                http.StatusUnauthorized,
	CodePermissionDenied:              http.StatusForbidden,
	CodeTenantAccessDenied:            http.StatusForbidden,
	CodeResourceNotFound:              http.StatusNotFound,
	CodeUserNotFound:                  http.StatusNotFound,
	CodeTenantNotFound:                http.StatusNotFound,
	CodeTenantSlugTaken:               http.StatusConflict,
	CodeUserAlreadyExists:             http.StatusConflict,
	CodeTenantMembershipAlreadyExists: http.StatusConflict,
	CodeRoleAlreadyExists:             http.StatusConflict,
	CodeRoleNotFound:                  http.StatusNotFound,
	CodePermissionNotFound:            http.StatusNotFound,
	CodeRoleAssignmentAlreadyExists:   http.StatusConflict,
	CodeRateLimited:                   http.StatusTooManyRequests,
	CodeServiceUnavailable:            http.StatusServiceUnavailable,
	CodeInternalError:                 http.StatusInternalServerError,
}

var publicMessageByCode = map[ErrorCode]string{
	CodeValidationFailed:              "The request failed validation.",
	CodeInvalidRequest:                "The request is invalid.",
	CodeInvalidCredentials:            "Invalid credentials.",
	CodeSessionExpired:                "The session has expired.",
	CodeSessionRevoked:                "The session has been revoked.",
	CodePermissionDenied:              "You do not have permission to perform this action.",
	CodeTenantAccessDenied:            "You do not have access to this tenant.",
	CodeResourceNotFound:              "The requested resource was not found.",
	CodeUserNotFound:                  "The requested user was not found.",
	CodeTenantNotFound:                "The requested tenant was not found.",
	CodeTenantSlugTaken:               "The tenant slug is already taken.",
	CodeUserAlreadyExists:             "The user already exists.",
	CodeTenantMembershipAlreadyExists: "The tenant membership already exists.",
	CodeRoleAlreadyExists:             "The role already exists.",
	CodeRoleNotFound:                  "The requested role was not found.",
	CodePermissionNotFound:            "The requested permission was not found.",
	CodeRoleAssignmentAlreadyExists:   "The role assignment already exists.",
	CodeRateLimited:                   "Too many requests. Please try again later.",
	CodeServiceUnavailable:            "The service is temporarily unavailable.",
	CodeInternalError:                 "An unexpected error occurred.",
}

func messageFor(code ErrorCode) string {
	return publicMessageByCode[code]
}
