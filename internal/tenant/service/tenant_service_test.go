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

// Feature 5 gave slug syntax its own error code; an empty or whitespace slug
// is now a TENANT_SLUG_INVALID rather than a generic validation failure.
func TestCreateTenantRejectsEmptySlug(t *testing.T) {
	service := NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, &tenantRepositoryFake{})
	_, err := service.Create(context.Background(), CreateTenantInput{Name: "Salon", Slug: "   ", CreatorUserID: testUserID})
	assertCreateTenantCode(t, err, apperrors.CodeTenantSlugInvalid)
}

// panicOnBeginTx fails the test if a transaction is ever opened, so these
// cases also prove no tenant, membership, or role assignment can be created.
func TestCreateTenantRejectsNonCanonicalSlugBeforeTransaction(t *testing.T) {
	for _, slug := range []string{"Acme", "acme salon", "acme_salon", "-acme", "acme-", "ac", "acmé"} {
		t.Run(slug, func(t *testing.T) {
			service := NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, &tenantRepositoryFake{})
			_, err := service.Create(context.Background(), CreateTenantInput{Name: "Salon", Slug: slug, CreatorUserID: testUserID})
			assertCreateTenantCode(t, err, apperrors.CodeTenantSlugInvalid)
		})
	}
}

func TestCreateTenantRejectsReservedSlugBeforeTransaction(t *testing.T) {
	for _, slug := range []string{"admin", "api", "login", "dashboard", "auth", "book", "settings"} {
		t.Run(slug, func(t *testing.T) {
			service := NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, &tenantRepositoryFake{})
			_, err := service.Create(context.Background(), CreateTenantInput{Name: "Salon", Slug: slug, CreatorUserID: testUserID})
			assertCreateTenantCode(t, err, apperrors.CodeTenantSlugInvalid)
		})
	}
}

func TestCreateTenantRejectsInvalidCreatorID(t *testing.T) {
	service := NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, &tenantRepositoryFake{})
	_, err := service.Create(context.Background(), CreateTenantInput{Name: "Salon", Slug: "salon", CreatorUserID: "not-a-uuid"})
	assertCreateTenantCode(t, err, apperrors.CodeInvalidRequest)
}

// F1: business_type is validated last among the pre-transaction checks, so
// these cases deliberately supply an otherwise-fully-valid request — a
// missing/invalid business_type on its own, with nothing else wrong, must
// still be caught before BeginTx.
func TestCreateTenantRejectsMissingBusinessTypeBeforeTransaction(t *testing.T) {
	service := NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, &tenantRepositoryFake{})
	_, err := service.Create(context.Background(), CreateTenantInput{Name: "Salon", Slug: "salon", CreatorUserID: testUserID})
	assertCreateTenantCode(t, err, apperrors.CodeValidationFailed)
}

func TestCreateTenantRejectsUnknownBusinessTypeBeforeTransaction(t *testing.T) {
	for _, businessType := range []string{"HOTEL ", "barbershop", "SUPER_ADMIN"} {
		t.Run(businessType, func(t *testing.T) {
			service := NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, &tenantRepositoryFake{})
			_, err := service.Create(context.Background(), CreateTenantInput{Name: "Salon", Slug: "salon", BusinessType: businessType, CreatorUserID: testUserID})
			assertCreateTenantCode(t, err, apperrors.CodeValidationFailed)
		})
	}
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
