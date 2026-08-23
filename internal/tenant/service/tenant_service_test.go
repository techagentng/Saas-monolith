package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

func TestCreateTenantRejectsEmptyName(t *testing.T) {
	service := NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, &tenantRepositoryFake{})
	_, err := service.Create(context.Background(), CreateTenantInput{Name: "   ", Slug: "salon", CreatorUserID: testUserID})
	assertCreateTenantCode(t, err, apperrors.CodeValidationFailed)
}

func TestCreateTenantRejectsEmptySlug(t *testing.T) {
	service := NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, &tenantRepositoryFake{})
	_, err := service.Create(context.Background(), CreateTenantInput{Name: "Salon", Slug: "   ", CreatorUserID: testUserID})
	assertCreateTenantCode(t, err, apperrors.CodeValidationFailed)
}

func TestCreateTenantRejectsInvalidCreatorID(t *testing.T) {
	service := NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, &tenantRepositoryFake{})
	_, err := service.Create(context.Background(), CreateTenantInput{Name: "Salon", Slug: "salon", CreatorUserID: "not-a-uuid"})
	assertCreateTenantCode(t, err, apperrors.CodeInvalidRequest)
}

func assertCreateTenantCode(t *testing.T, err error, expected apperrors.ErrorCode) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != expected {
		t.Fatalf("error = %v, want %q", err, expected)
	}
}

// panicOnBeginTx proves validation failures never reach the database: any
// call to BeginTx fails the test immediately.
type panicOnBeginTx struct{ t *testing.T }

func (p *panicOnBeginTx) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	p.t.Fatal("BeginTx called despite invalid input")
	return nil, nil
}
