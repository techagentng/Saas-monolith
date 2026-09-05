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
	CodeServiceNotFound               ErrorCode = "SERVICE_NOT_FOUND"
	CodeStaffNotFound                 ErrorCode = "STAFF_NOT_FOUND"
	CodeCategoryNotFound              ErrorCode = "CATEGORY_NOT_FOUND"
	CodeTenantSlugTaken               ErrorCode = "TENANT_SLUG_TAKEN"
	CodeTenantSlugInvalid             ErrorCode = "TENANT_SLUG_INVALID"
	CodeUserAlreadyExists             ErrorCode = "USER_ALREADY_EXISTS"
	CodeTenantMembershipAlreadyExists ErrorCode = "TENANT_MEMBERSHIP_ALREADY_EXISTS"
	CodeRoleAlreadyExists             ErrorCode = "ROLE_ALREADY_EXISTS"
	CodeRoleNotFound                  ErrorCode = "ROLE_NOT_FOUND"
	CodePermissionNotFound            ErrorCode = "PERMISSION_NOT_FOUND"
	CodeRoleAssignmentAlreadyExists   ErrorCode = "ROLE_ASSIGNMENT_ALREADY_EXISTS"
	// CodeBookingSlotUnavailable is returned when a requested appointment time
	// cannot be booked — it is in the past, outside working hours, across a
	// split-shift gap, or (the concurrency case) another customer's booking
	// now overlaps it. All of these resolve the same way for the customer:
	// pick another time. Maps to 409, distinct from a 400 validation failure.
	CodeBookingSlotUnavailable ErrorCode = "BOOKING_SLOT_UNAVAILABLE"
	// CodeBookingNotFound is returned when a booking id does not resolve within
	// the caller's tenant — whether it never existed or belongs to another
	// tenant, which are deliberately indistinguishable. Maps to 404.
	CodeBookingNotFound ErrorCode = "BOOKING_NOT_FOUND"
	// Google OAuth / OpenID Connect. These exist because the generic codes
	// cannot distinguish causes the frontend must present differently: a
	// denied consent screen is the user's own choice and deserves no alarming
	// copy, while an unverified Google email is a hard stop the user cannot
	// retry their way out of. No code here ever carries a Google error string,
	// an authorization code, or a token.
	CodeOAuthStateInvalid         ErrorCode = "OAUTH_STATE_INVALID"
	CodeOAuthDenied               ErrorCode = "OAUTH_DENIED"
	CodeOAuthExchangeFailed       ErrorCode = "OAUTH_EXCHANGE_FAILED"
	CodeOAuthInvalidIdentityToken ErrorCode = "OAUTH_INVALID_IDENTITY_TOKEN"
	CodeOAuthEmailUnverified      ErrorCode = "OAUTH_EMAIL_UNVERIFIED"
	CodeExternalIdentityConflict  ErrorCode = "EXTERNAL_IDENTITY_CONFLICT"
	CodeRateLimited               ErrorCode = "RATE_LIMITED"
	CodeServiceUnavailable        ErrorCode = "SERVICE_UNAVAILABLE"
	CodeInternalError             ErrorCode = "INTERNAL_ERROR"
)
