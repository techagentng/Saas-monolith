package errors

// ErrorCode is the stable machine-readable identity of an application error.
type ErrorCode string

const (
	CodeValidationFailed              ErrorCode = "VALIDATION_FAILED"
	CodeInvalidRequest                ErrorCode = "INVALID_REQUEST"
	CodeInvalidCredentials            ErrorCode = "INVALID_CREDENTIALS"
	CodeSessionExpired                ErrorCode = "SESSION_EXPIRED"
	CodeSessionRevoked                ErrorCode = "SESSION_REVOKED"
	CodePermissionDenied              ErrorCode = "PERMISSION_DENIED"
	CodeTenantAccessDenied            ErrorCode = "TENANT_ACCESS_DENIED"
	CodeResourceNotFound              ErrorCode = "RESOURCE_NOT_FOUND"
	CodeUserNotFound                  ErrorCode = "USER_NOT_FOUND"
	CodeTenantNotFound                ErrorCode = "TENANT_NOT_FOUND"
	CodeUserAlreadyExists             ErrorCode = "USER_ALREADY_EXISTS"
	CodeTenantMembershipAlreadyExists ErrorCode = "TENANT_MEMBERSHIP_ALREADY_EXISTS"
	CodeRoleAlreadyExists             ErrorCode = "ROLE_ALREADY_EXISTS"
	CodeRateLimited                   ErrorCode = "RATE_LIMITED"
	CodeServiceUnavailable            ErrorCode = "SERVICE_UNAVAILABLE"
	CodeInternalError                 ErrorCode = "INTERNAL_ERROR"
)
