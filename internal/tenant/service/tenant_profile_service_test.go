package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

const profileTenantID = "550e8400-e29b-41d4-a716-446655440900"

func ptr(value string) *string { return &value }

// recordingTenantRepository captures whether UpdateProfile was reached and
// with what typed input, so validation tests can prove rejected requests
// never touch persistence.
type recordingTenantRepository struct {
	tenantRepositoryFake

	calls     int
	lastID    string
	lastInput repository.TenantProfileUpdate
	returnErr error
	returned  *model.Tenant
}

func (r *recordingTenantRepository) UpdateProfile(_ context.Context, tenantID string, update repository.TenantProfileUpdate) (*model.Tenant, error) {
	r.calls++
	r.lastID = tenantID
	r.lastInput = update
	if r.returnErr != nil {
		return nil, r.returnErr
	}
	if r.returned != nil {
		return r.returned, nil
	}
	return &model.Tenant{ID: tenantID, Name: "Acme Salon", Slug: "acme-salon", Status: model.StatusActive}, nil
}

func newProfileService(t *testing.T, repo *recordingTenantRepository) TenantService {
	t.Helper()
	return NewTenantService(&panicOnBeginTx{t: t}, &userRepositoryFake{}, repo)
}

func assertProfileCode(t *testing.T, err error, expected apperrors.ErrorCode) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != expected {
		t.Fatalf("error = %v, want %q", err, expected)
	}
}

// assertRejectedBeforePersistence proves a validation failure never reached
// the repository, so no data could have been mutated.
func assertRejectedBeforePersistence(t *testing.T, repo *recordingTenantRepository) {
	t.Helper()
	if repo.calls != 0 {
		t.Fatalf("repository UpdateProfile called %d times; rejected requests must not persist", repo.calls)
	}
}

// --- Name --------------------------------------------------------------------

func TestUpdateProfileAcceptsValidName(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		Name: ptr("  Acme Beauty Studio  "),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if repo.lastInput.Name == nil || *repo.lastInput.Name != "Acme Beauty Studio" {
		t.Fatalf("Name passed to repository = %v, want trimmed %q", repo.lastInput.Name, "Acme Beauty Studio")
	}
}

func TestUpdateProfileRejectsWhitespaceOnlyName(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		Name: ptr("   "),
	})
	assertProfileCode(t, err, apperrors.CodeValidationFailed)
	assertRejectedBeforePersistence(t, repo)
}

func TestUpdateProfileRejectsOverlengthName(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		Name: ptr(strings.Repeat("a", 256)),
	})
	assertProfileCode(t, err, apperrors.CodeValidationFailed)
	assertRejectedBeforePersistence(t, repo)
}

// --- Description --------------------------------------------------------------

func TestUpdateProfileAcceptsValidDescription(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		Description: ptr("Full service beauty salon"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if repo.lastInput.Description == nil || *repo.lastInput.Description != "Full service beauty salon" {
		t.Fatalf("Description = %v", repo.lastInput.Description)
	}
}

// Product decision: an empty description is a legitimate state ("no
// description"), not a validation error. This is distinct from null-clearing,
// which Feature 4 deliberately does not implement.
func TestUpdateProfileAcceptsEmptyDescriptionAsIntentionallyBlank(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		Description: ptr(""),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v, want empty description accepted", err)
	}
	if repo.lastInput.Description == nil || *repo.lastInput.Description != "" {
		t.Fatalf("Description = %v, want empty string persisted", repo.lastInput.Description)
	}
}

func TestUpdateProfileRejectsOverlengthDescription(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		Description: ptr(strings.Repeat("d", 1001)),
	})
	assertProfileCode(t, err, apperrors.CodeValidationFailed)
	assertRejectedBeforePersistence(t, repo)
}

// --- Contact email ------------------------------------------------------------

func TestUpdateProfileAcceptsValidContactEmail(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		ContactEmail: ptr("hello@acme.test"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if repo.lastInput.ContactEmail == nil || *repo.lastInput.ContactEmail != "hello@acme.test" {
		t.Fatalf("ContactEmail = %v", repo.lastInput.ContactEmail)
	}
}

func TestUpdateProfileRejectsInvalidContactEmail(t *testing.T) {
	for _, invalid := range []string{"not-an-email", "@example.com", "user@", ""} {
		repo := &recordingTenantRepository{}
		_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
			ContactEmail: ptr(invalid),
		})
		assertProfileCode(t, err, apperrors.CodeValidationFailed)
		assertRejectedBeforePersistence(t, repo)
	}
}

// The tenant's business contact email is independent of any user login
// identity; the profile update carries no user fields at all.
func TestUpdateProfileContactEmailIsBusinessContactNotLogin(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		ContactEmail: ptr("business@acme.test"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if repo.lastID != profileTenantID {
		t.Fatalf("repository targeted %q, want tenant %q", repo.lastID, profileTenantID)
	}
}

// --- Contact phone ------------------------------------------------------------

func TestUpdateProfileAcceptsValidContactPhone(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		ContactPhone: ptr("+2348012345678"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if repo.lastInput.ContactPhone == nil || *repo.lastInput.ContactPhone != "+2348012345678" {
		t.Fatalf("ContactPhone = %v", repo.lastInput.ContactPhone)
	}
}

func TestUpdateProfileRejectsOverlengthContactPhone(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		ContactPhone: ptr(strings.Repeat("9", 21)),
	})
	assertProfileCode(t, err, apperrors.CodeValidationFailed)
	assertRejectedBeforePersistence(t, repo)
}

func TestUpdateProfileRejectsEmptyContactPhone(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		ContactPhone: ptr("   "),
	})
	assertProfileCode(t, err, apperrors.CodeValidationFailed)
	assertRejectedBeforePersistence(t, repo)
}

// --- Timezone -------------------------------------------------------------------

func TestUpdateProfileAcceptsValidIANATimezone(t *testing.T) {
	for _, zone := range []string{"Africa/Lagos", "Europe/London", "America/New_York", "UTC"} {
		repo := &recordingTenantRepository{}
		_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
			Timezone: ptr(zone),
		})
		if err != nil {
			t.Fatalf("UpdateProfile() timezone %q error = %v", zone, err)
		}
		if repo.lastInput.Timezone == nil || *repo.lastInput.Timezone != zone {
			t.Fatalf("Timezone = %v, want %q", repo.lastInput.Timezone, zone)
		}
	}
}

func TestUpdateProfileRejectsInvalidTimezone(t *testing.T) {
	for _, zone := range []string{"Not/AZone", "Mars/Olympus", "", "+01:00"} {
		repo := &recordingTenantRepository{}
		_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
			Timezone: ptr(zone),
		})
		assertProfileCode(t, err, apperrors.CodeValidationFailed)
		assertRejectedBeforePersistence(t, repo)
	}
}

// --- Patch shape ------------------------------------------------------------------

func TestUpdateProfileRejectsEmptyPatch(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{})
	assertProfileCode(t, err, apperrors.CodeValidationFailed)
	assertRejectedBeforePersistence(t, repo)
}

func TestUpdateProfileRejectsMalformedTenantID(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), "not-a-uuid", UpdateTenantProfileRequest{
		Name: ptr("Acme"),
	})
	assertProfileCode(t, err, apperrors.CodeInvalidRequest)
	assertRejectedBeforePersistence(t, repo)
}

func TestUpdateProfileOmittedFieldsAreNotSentToRepository(t *testing.T) {
	repo := &recordingTenantRepository{}
	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		Name: ptr("Only Name"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if repo.lastInput.Description != nil || repo.lastInput.ContactEmail != nil ||
		repo.lastInput.ContactPhone != nil || repo.lastInput.Timezone != nil {
		t.Fatalf("omitted fields leaked into repository input: %#v", repo.lastInput)
	}
}

func TestUpdateProfilePropagatesRepositoryError(t *testing.T) {
	repoErr := apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	repo := &recordingTenantRepository{returnErr: repoErr}

	_, err := newProfileService(t, repo).UpdateProfile(context.Background(), profileTenantID, UpdateTenantProfileRequest{
		Name: ptr("Acme"),
	})
	assertProfileCode(t, err, apperrors.CodeTenantNotFound)
	if !errors.Is(err, repoErr) {
		t.Fatalf("error = %v, want repository error preserved in chain", err)
	}
}
