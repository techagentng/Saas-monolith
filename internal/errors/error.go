package errors

import "fmt"

// AppError carries application semantics while retaining an optional cause.
type AppError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func New(code ErrorCode, message string, cause error) *AppError {
	return &AppError{Code: code, Message: message, Err: cause}
}

func (e *AppError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
