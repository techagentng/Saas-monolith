package service

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

const (
	staffID       = "550e8400-e29b-41d4-a716-446655448001"
	linkedUserID  = "550e8400-e29b-41d4-a716-446655448002"
	ownerUserID   = "550e8400-e29b-41d4-a716-446655448003"
	otherServiceA = "550e8400-e29b-41d4-a716-446655448004"
	otherServiceB = "550e8400-e29b-41d4-a716-446655448005"
)

// --- fakes -------------------------------------------------------------------

type fakeStaffRepository struct {
	profiles map[string]*model.StaffProfile

	created      *model.StaffProfile
	createCalls  int
	updateCalls  int
	archiveCalls int
	lastTenantID string
	lastFilter   repository.StaffListFilter

	createErr error
}

func newFakeStaffRepository() *fakeStaffRepository {
	return &fakeStaffRepository{profiles: map[string]*model.StaffProfile{}}
}

func (r *fakeStaffRepository) Create(_ context.Context, profile *model.StaffProfile) (*model.StaffProfile, error) {
	r.createCalls++
	if r.createErr != nil {
		return nil, r.createErr
	}
	stored := *profile
	if stored.Status == "" {
		stored.Status = model.StatusActive
	}
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	r.profiles[stored.ID] = &stored
	r.created = profile
	return &stored, nil
}

func (r *fakeStaffRepository) FindByID(_ context.Context, tenantID string, id string) (*model.StaffProfile, error) {
	r.lastTenantID = tenantID
	stored, ok := r.profiles[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)
	}
	return stored, nil
}

func (r *fakeStaffRepository) ListByTenant(_ context.Context, tenantID string, filter repository.StaffListFilter) ([]*model.StaffProfile, error) {
	r.lastTenantID = tenantID
	r.lastFilter = filter
	var result []*model.StaffProfile
	for _, stored := range r.profiles {
		if stored.TenantID != tenantID {
			continue
		}
		if filter.Status != nil && stored.Status != *filter.Status {
			continue
		}
		result = append(result, stored)
	}
	return result, nil
}

func (r *fakeStaffRepository) Update(_ context.Context, tenantID string, id string, update repository.StaffUpdate) (*model.StaffProfile, error) {
	r.updateCalls++
	stored, ok := r.profiles[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)
	}
	if update.DisplayName != nil {
		stored.DisplayName = *update.DisplayName
	}
	if update.Bio != nil {
		stored.Bio = update.Bio
	}
	if update.IsBookable != nil {
		stored.IsBookable = *update.IsBookable
	}
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

func (r *fakeStaffRepository) Archive(_ context.Context, tenantID string, id string) (*model.StaffProfile, error) {
	r.archiveCalls++
	stored, ok := r.profiles[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)
	}
	stored.Status = model.StatusArchived
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

type fakeCapabilityRepository struct {
	assignments map[string][]string
	listCalls   int
}

func newFakeCapabilityRepository() *fakeCapabilityRepository {
	return &fakeCapabilityRepository{assignments: map[string][]string{}}
}

func (r *fakeCapabilityRepository) ListServiceIDs(_ context.Context, _ string, staffID string) ([]string, error) {
	r.listCalls++
	ids := r.assignments[staffID]
	if ids == nil {
		return []string{}, nil
	}
	return append([]string{}, ids...), nil
}

func (r *fakeCapabilityRepository) ListStaffIDsForService(_ context.Context, _ string, serviceID string) ([]string, error) {
	r.listCalls++
	var staffIDs []string
	for staffID, ids := range r.assignments {
		for _, id := range ids {
			if id == serviceID {
				staffIDs = append(staffIDs, staffID)
				break
			}
		}
	}
	sort.Strings(staffIDs)
	return staffIDs, nil
}

func (r *fakeCapabilityRepository) DeleteAll(_ context.Context, _ string, staffID string) error {
	delete(r.assignments, staffID)
	return nil
}

func (r *fakeCapabilityRepository) Assign(_ context.Context, _ string, staffID string, serviceID string) error {
	r.assignments[staffID] = append(r.assignments[staffID], serviceID)
	return nil
}

type fakeMembershipReader struct {
	membership *tenantmodel.TenantMembership
	findCalls  int
	findErr    error
}

func (r *fakeMembershipReader) FindByTenantAndUser(_ context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error) {
	r.findCalls++
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.membership == nil || r.membership.TenantID != tenantID || r.membership.UserID != userID {
		return nil, nil
	}
	return r.membership, nil
}

func activeMember(userID string) *fakeMembershipReader {
	return &fakeMembershipReader{membership: &tenantmodel.TenantMembership{
		TenantID: tenantA, UserID: userID, Status: tenantmodel.MembershipStatusActive,
	}}
}

// staffFixture wires a StaffService over fakes. db is nil because every test
// below that reaches a transaction is an integration test; the service-layer
// tests here all fail before BeginTx, which is itself the property under test.
type staffFixture struct {
	staff        *fakeStaffRepository
	capabilities *fakeCapabilityRepository
	services     *fakeServiceRepository
	memberships  *fakeMembershipReader
	service      StaffService
}

func newStaffFixture(t *testing.T, memberships *fakeMembershipReader) *staffFixture {
	t.Helper()
	staff := newFakeStaffRepository()
	capabilities := newFakeCapabilityRepository()
	services := newFakeServiceRepository()
	if memberships == nil {
		memberships = &fakeMembershipReader{}
	}
	return &staffFixture{
		staff:        staff,
		capabilities: capabilities,
		services:     services,
		memberships:  memberships,
		service:      NewStaffService(nilTxBeginner{}, staff, capabilities, services, memberships),
	}
}

// nilTxBeginner fails loudly if a test reaches a transaction it should not.
type nilTxBeginner struct{}

func (nilTxBeginner) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	return nil, errors.New("BeginTx reached in a unit test — capability replacement must validate before opening a transaction")
}

// --- create ------------------------------------------------------------------

func TestCreateNonLoginStaffNeedsNoUserAccount(t *testing.T) {
	fixture := newStaffFixture(t, nil)

	created, err := fixture.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "  Ada  "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.UserID != nil {
		t.Fatalf("UserID = %v, want nil for a non-login worker", *created.UserID)
	}
	if created.DisplayName != "Ada" {
		t.Fatalf("DisplayName = %q, want the trimmed value", created.DisplayName)
	}
	if created.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE", created.Status)
	}
	if !created.IsBookable {
		t.Fatal("IsBookable = false, want true by default")
	}
	if fixture.memberships.findCalls != 0 {
		t.Fatal("Create() consulted membership for an unlinked profile")
	}
	// Status must be left for the repository to default, exactly as the catalog
	// does — the client never chooses it and neither does this layer.
	if fixture.staff.created.Status != "" {
		t.Fatalf("profile passed to the repository had Status %q preset", fixture.staff.created.Status)
	}
}

func TestCreateHonoursExplicitIsBookableFalse(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	notBookable := false

	created, err := fixture.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "Front Desk", IsBookable: &notBookable})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.IsBookable {
		t.Fatal("IsBookable = true, want false — a receptionist is an ACTIVE profile who is not bookable")
	}
	if created.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE — not bookable is not the same as archived", created.Status)
	}
}

func TestCreateLinkedStaffRequiresAnActiveMembership(t *testing.T) {
	fixture := newStaffFixture(t, activeMember(linkedUserID))

	created, err := fixture.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "Ada", UserID: strPtr(linkedUserID)})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.UserID == nil || *created.UserID != linkedUserID {
		t.Fatalf("UserID = %v, want the linked user", created.UserID)
	}
}

func TestCreateRejectsAUserWithNoMembership(t *testing.T) {
	// No membership at all — the same shape as a nonexistent user, deliberately.
	fixture := newStaffFixture(t, &fakeMembershipReader{})

	_, err := fixture.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "Ada", UserID: strPtr(linkedUserID)})
	assertCode(t, err, apperrors.CodeValidationFailed, "Create(non-member user)")
	if fixture.staff.createCalls != 0 {
		t.Fatal("Create() persisted a profile linked to a non-member")
	}
}

func TestCreateRejectsAUserWhoseMembershipIsDisabled(t *testing.T) {
	memberships := &fakeMembershipReader{membership: &tenantmodel.TenantMembership{
		TenantID: tenantA, UserID: linkedUserID, Status: tenantmodel.MembershipStatusDisabled,
	}}
	fixture := newStaffFixture(t, memberships)

	_, err := fixture.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "Ada", UserID: strPtr(linkedUserID)})
	assertCode(t, err, apperrors.CodeValidationFailed, "Create(disabled membership)")
	if fixture.staff.createCalls != 0 {
		t.Fatal("Create() persisted a profile linked to a disabled member")
	}
}

// A nonexistent user and a user who simply is not a member must be
// indistinguishable, so this endpoint cannot probe which user IDs exist.
func TestCreateDoesNotDiscloseWhetherAUserExists(t *testing.T) {
	nonMember := newStaffFixture(t, &fakeMembershipReader{})
	_, missingErr := nonMember.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "Ada", UserID: strPtr(linkedUserID)})

	disabled := newStaffFixture(t, &fakeMembershipReader{membership: &tenantmodel.TenantMembership{
		TenantID: tenantA, UserID: ownerUserID, Status: tenantmodel.MembershipStatusDisabled,
	}})
	_, disabledErr := disabled.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "Ada", UserID: strPtr(ownerUserID)})

	if missingErr.Error() != disabledErr.Error() {
		t.Fatalf("distinguishable errors disclose user existence:\n  %v\n  %v", missingErr, disabledErr)
	}
}

func TestCreateRejectsInvalidDisplayNameBeforeTouchingPersistence(t *testing.T) {
	fixture := newStaffFixture(t, activeMember(linkedUserID))

	_, err := fixture.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "   "})
	assertCode(t, err, apperrors.CodeValidationFailed, "Create(empty display name)")
	if fixture.staff.createCalls != 0 {
		t.Fatal("Create() reached the repository with an invalid display name")
	}
	if fixture.memberships.findCalls != 0 {
		t.Fatal("Create() checked membership before validating the display name")
	}
}

func TestCreateRejectsMalformedTenantAndUserIdentifiers(t *testing.T) {
	fixture := newStaffFixture(t, nil)

	if _, err := fixture.service.Create(context.Background(), "not-a-uuid", CreateStaffInput{DisplayName: "Ada"}); err == nil {
		t.Fatal("Create() accepted a malformed tenant id")
	} else {
		assertCode(t, err, apperrors.CodeInvalidRequest, "Create(malformed tenant id)")
	}

	if _, err := fixture.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "Ada", UserID: strPtr("not-a-uuid")}); err == nil {
		t.Fatal("Create() accepted a malformed user id")
	} else {
		assertCode(t, err, apperrors.CodeValidationFailed, "Create(malformed user id)")
	}
}

func TestCreateStaffPropagatesRepositoryFailure(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.createErr = errors.New("connection reset")

	_, err := fixture.service.Create(context.Background(), tenantA, CreateStaffInput{DisplayName: "Ada"})
	if err == nil {
		t.Fatal("Create() swallowed a repository failure")
	}
	if mapped := apperrors.Map(err); mapped.Code != apperrors.CodeInternalError {
		t.Fatalf("mapped code = %s, want INTERNAL_ERROR", mapped.Code)
	}
}

// --- owner as technician (mandatory acceptance scenario) ---------------------

// A BUSINESS_OWNER gets a bookable staff profile without acquiring the STAFF
// role. The proof that no RBAC mutation happens is structural: StaffService is
// constructed with no role or permission repository at all, so there is nothing
// it could write to user_roles even if it wanted to.
func TestBusinessOwnerBecomesBookableWithoutGainingTheStaffRole(t *testing.T) {
	fixture := newStaffFixture(t, activeMember(ownerUserID))

	created, err := fixture.service.Create(context.Background(), tenantA, CreateStaffInput{
		DisplayName: "Nnamdi", UserID: strPtr(ownerUserID),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.UserID == nil || *created.UserID != ownerUserID {
		t.Fatalf("UserID = %v, want the owner's user id", created.UserID)
	}
	if !created.IsBookable {
		t.Fatal("the owner's profile is not bookable")
	}
	if created.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE", created.Status)
	}
	// The only membership interaction is a read. Nothing about the user's roles
	// or membership was written.
	if fixture.memberships.findCalls != 1 {
		t.Fatalf("membership reads = %d, want exactly 1 (a read, never a write)", fixture.memberships.findCalls)
	}
}

// --- list --------------------------------------------------------------------

func TestStaffListDefaultsToActiveOnly(t *testing.T) {
	fixture := newStaffFixture(t, nil)

	if _, err := fixture.service.List(context.Background(), tenantA, ""); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if fixture.staff.lastFilter.Status == nil || *fixture.staff.lastFilter.Status != model.StatusActive {
		t.Fatalf("filter = %v, want ACTIVE by default", fixture.staff.lastFilter.Status)
	}
}

func TestStaffListStatusFilters(t *testing.T) {
	for _, test := range []struct {
		raw     string
		wantAll bool
		want    model.Status
	}{
		{"ACTIVE", false, model.StatusActive},
		{"ARCHIVED", false, model.StatusArchived},
		{"ALL", true, ""},
	} {
		t.Run(test.raw, func(t *testing.T) {
			fixture := newStaffFixture(t, nil)
			if _, err := fixture.service.List(context.Background(), tenantA, test.raw); err != nil {
				t.Fatalf("List(%q) error = %v", test.raw, err)
			}
			if test.wantAll {
				if fixture.staff.lastFilter.Status != nil {
					t.Fatalf("filter = %v, want every status for ALL", *fixture.staff.lastFilter.Status)
				}
				return
			}
			if fixture.staff.lastFilter.Status == nil || *fixture.staff.lastFilter.Status != test.want {
				t.Fatalf("filter = %v, want %q", fixture.staff.lastFilter.Status, test.want)
			}
		})
	}
}

func TestStaffListRejectsUnknownStatusFilter(t *testing.T) {
	fixture := newStaffFixture(t, nil)

	_, err := fixture.service.List(context.Background(), tenantA, "DISABLED")
	assertCode(t, err, apperrors.CodeValidationFailed, "List(unknown filter)")
}

func TestStaffListIsScopedToTheTenant(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles["a"] = &model.StaffProfile{ID: "a", TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}
	fixture.staff.profiles["b"] = &model.StaffProfile{ID: "b", TenantID: tenantB, DisplayName: "Rival", Status: model.StatusActive}

	result, err := fixture.service.List(context.Background(), tenantA, "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result) != 1 || result[0].TenantID != tenantA {
		t.Fatalf("List() returned %d rows including another tenant's", len(result))
	}
}

// --- get / update / archive --------------------------------------------------

func TestGetTreatsAnotherTenantsProfileAsNotFound(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}

	_, err := fixture.service.Get(context.Background(), tenantB, staffID)
	assertCode(t, err, apperrors.CodeStaffNotFound, "Get(cross-tenant)")
}

func TestUpdateStaffAppliesOnlySuppliedFields(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", IsBookable: true, Status: model.StatusActive}

	name := "  Ada Obi  "
	updated, err := fixture.service.Update(context.Background(), tenantA, staffID, UpdateStaffInput{DisplayName: &name})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.DisplayName != "Ada Obi" {
		t.Fatalf("DisplayName = %q, want the trimmed value", updated.DisplayName)
	}
	if !updated.IsBookable {
		t.Fatal("Update() changed IsBookable, which was not supplied")
	}
	if updated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want it untouched — archiving owns lifecycle", updated.Status)
	}
}

func TestUpdateCanToggleBookabilityWithoutArchiving(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", IsBookable: true, Status: model.StatusActive}

	notBookable := false
	updated, err := fixture.service.Update(context.Background(), tenantA, staffID, UpdateStaffInput{IsBookable: &notBookable})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.IsBookable {
		t.Fatal("IsBookable was not cleared")
	}
	if updated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE — pausing bookings must not archive anyone", updated.Status)
	}
}

func TestUpdateStaffRejectsAnEmptyPatch(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}

	_, err := fixture.service.Update(context.Background(), tenantA, staffID, UpdateStaffInput{})
	assertCode(t, err, apperrors.CodeValidationFailed, "Update(empty patch)")
	if fixture.staff.updateCalls != 0 {
		t.Fatal("Update() reached the repository with nothing to write")
	}
}

func TestUpdateTreatsAnotherTenantsProfileAsNotFound(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}

	name := "Hijacked"
	_, err := fixture.service.Update(context.Background(), tenantB, staffID, UpdateStaffInput{DisplayName: &name})
	assertCode(t, err, apperrors.CodeStaffNotFound, "Update(cross-tenant)")
	if fixture.staff.profiles[staffID].DisplayName != "Ada" {
		t.Fatalf("a cross-tenant update mutated the profile: %q", fixture.staff.profiles[staffID].DisplayName)
	}
}

func TestArchiveIsIdempotentAndKeepsTheRow(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}

	archived, err := fixture.service.Archive(context.Background(), tenantA, staffID)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if archived.Status != model.StatusArchived {
		t.Fatalf("Status = %q, want ARCHIVED", archived.Status)
	}
	if _, present := fixture.staff.profiles[staffID]; !present {
		t.Fatal("Archive() removed the row — archiving must never delete")
	}

	callsAfterFirst := fixture.staff.archiveCalls
	if _, err := fixture.service.Archive(context.Background(), tenantA, staffID); err != nil {
		t.Fatalf("second Archive() error = %v, want idempotent success", err)
	}
	if fixture.staff.archiveCalls != callsAfterFirst {
		t.Fatal("Archive() re-persisted an already archived profile")
	}
}

// --- capabilities ------------------------------------------------------------

func TestReplaceCapabilitiesRejectsAServiceFromAnotherTenant(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}
	// The service genuinely exists — under tenant B.
	fixture.services.services[otherServiceA] = &model.Service{ID: otherServiceA, TenantID: tenantB, Name: "Rival Service", Status: model.StatusActive}

	_, err := fixture.service.ReplaceCapabilities(context.Background(), tenantA, staffID, []string{otherServiceA})
	assertCode(t, err, apperrors.CodeValidationFailed, "ReplaceCapabilities(cross-tenant service)")
	// nilTxBeginner would have errored differently had a transaction opened;
	// reaching a validation failure proves the check happens first.
	if len(fixture.capabilities.assignments[staffID]) != 0 {
		t.Fatal("a cross-tenant service was assigned")
	}
}

func TestReplaceCapabilitiesRejectsAnUnknownService(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}

	_, err := fixture.service.ReplaceCapabilities(context.Background(), tenantA, staffID, []string{otherServiceA})
	assertCode(t, err, apperrors.CodeValidationFailed, "ReplaceCapabilities(unknown service)")
}

// The whole set is validated before anything is written, so an invalid member
// anywhere in the list leaves the existing assignments untouched.
func TestReplaceCapabilitiesValidatesTheWholeSetBeforeWriting(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}
	fixture.services.services[otherServiceA] = &model.Service{ID: otherServiceA, TenantID: tenantA, Name: "Manicure", Status: model.StatusActive}
	fixture.capabilities.assignments[staffID] = []string{otherServiceA}

	// First entry valid, second belongs to another tenant.
	fixture.services.services[otherServiceB] = &model.Service{ID: otherServiceB, TenantID: tenantB, Name: "Rival", Status: model.StatusActive}
	_, err := fixture.service.ReplaceCapabilities(context.Background(), tenantA, staffID, []string{otherServiceA, otherServiceB})
	if err == nil {
		t.Fatal("ReplaceCapabilities() accepted a set containing another tenant's service")
	}

	existing := fixture.capabilities.assignments[staffID]
	if len(existing) != 1 || existing[0] != otherServiceA {
		t.Fatalf("the previous capability set was disturbed by a rejected replacement: %v", existing)
	}
}

func TestReplaceCapabilitiesRejectsAMalformedServiceID(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}

	_, err := fixture.service.ReplaceCapabilities(context.Background(), tenantA, staffID, []string{"not-a-uuid"})
	assertCode(t, err, apperrors.CodeValidationFailed, "ReplaceCapabilities(malformed service id)")
}

func TestReplaceCapabilitiesRejectsAnUnknownStaffMember(t *testing.T) {
	fixture := newStaffFixture(t, nil)

	_, err := fixture.service.ReplaceCapabilities(context.Background(), tenantA, staffID, nil)
	assertCode(t, err, apperrors.CodeStaffNotFound, "ReplaceCapabilities(unknown staff)")
}

func TestReplaceCapabilitiesRejectsAnotherTenantsStaffMember(t *testing.T) {
	fixture := newStaffFixture(t, nil)
	fixture.staff.profiles[staffID] = &model.StaffProfile{ID: staffID, TenantID: tenantA, DisplayName: "Ada", Status: model.StatusActive}

	_, err := fixture.service.ReplaceCapabilities(context.Background(), tenantB, staffID, nil)
	assertCode(t, err, apperrors.CodeStaffNotFound, "ReplaceCapabilities(cross-tenant staff)")
}

func TestListCapabilitiesRejectsAnUnknownStaffMemberRatherThanReturningEmpty(t *testing.T) {
	fixture := newStaffFixture(t, nil)

	_, err := fixture.service.ListCapabilities(context.Background(), tenantA, staffID)
	assertCode(t, err, apperrors.CodeStaffNotFound, "ListCapabilities(unknown staff)")
	if fixture.capabilities.listCalls != 0 {
		t.Fatal("ListCapabilities() queried capabilities for a profile that does not exist")
	}
}

func strPtr(value string) *string { return &value }
