package model

import (
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

func TestValidateBusinessTypeAcceptsEachApprovedValue(t *testing.T) {
	for _, want := range []BusinessType{BusinessTypeNailTechnician, BusinessTypeRestaurant, BusinessTypeHotel, BusinessTypeTransport} {
		t.Run(string(want), func(t *testing.T) {
			got, err := ValidateBusinessType(string(want))
			if err != nil {
				t.Fatalf("ValidateBusinessType(%q) error = %v", want, err)
			}
			if got != want {
				t.Fatalf("ValidateBusinessType(%q) = %q, want unchanged", want, got)
			}
		})
	}
}

func TestValidateBusinessTypeRejectsUnknownValue(t *testing.T) {
	for _, value := range []string{"BARBERSHOP", "hotel", " HOTEL", "HOTEL ", "nail_technician", "unknown"} {
		t.Run(value, func(t *testing.T) {
			_, err := ValidateBusinessType(value)
			assertBusinessTypeCode(t, err, apperrors.CodeValidationFailed)
		})
	}
}

func TestValidateBusinessTypeRejectsEmptyValue(t *testing.T) {
	_, err := ValidateBusinessType("")
	assertBusinessTypeCode(t, err, apperrors.CodeValidationFailed)
}

func assertBusinessTypeCode(t *testing.T, err error, expected apperrors.ErrorCode) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != expected {
		t.Fatalf("error = %v, want %q", err, expected)
	}
}
