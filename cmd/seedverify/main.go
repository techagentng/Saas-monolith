// Command seedverify creates deterministic LOCAL DEVELOPMENT verification data
// for the booking SaaS: one dashboard owner, a fully public NAIL_TECHNICIAN
// tenant (Luxe Nails Studio) with real services, technicians, capabilities and
// recurring working hours, an empty public nail tenant (Empty Nails), and a
// HOTEL tenant (Grand Hotel) for the unsupported-vertical state.
//
// It is a thin driver over the SAME config, database, repository and service
// packages the API itself uses — it invents no fields and duplicates no rules.
// It never drops or truncates anything, and every run uses timestamp-suffixed
// slugs and owner email so it is safe to run repeatedly.
//
// It refuses to run against anything that looks like production: a production
// APP_ENV, a non-local Postgres host, a managed-database hostname, or an SSL
// mode a local database would not use. It is not wired into the server.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	// Windows / minimal containers often have no system zoneinfo; the tenant
	// profile update rejects a timezone time.LoadLocation cannot resolve, so
	// without this the Africa/Lagos timezone below could be refused.
	_ "time/tzdata"

	"github.com/joho/godotenv"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"

	"github.com/techagentng/saas-monolith/internal/config"
	"github.com/techagentng/saas-monolith/internal/database"

	identityrepository "github.com/techagentng/saas-monolith/internal/identity/repository"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"

	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"

	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// devOwnerPassword is a fixed, obviously-development credential. It is printed
// in cleartext at the end on purpose — it protects nothing but local test
// data. It satisfies model.ValidatePassword (8-128 chars).
const devOwnerPassword = "dev-verify-Passw0rd!"

// bookingDays is the recurring weekly schedule the technicians get: Monday
// through Saturday, a morning and an afternoon block with a lunch gap between
// them (which also makes the S7 split-shift path testable). Because it is
// every weekday, whatever upcoming day the tester picks has availability — no
// historical or hardcoded date is involved.
var bookingDays = []string{"MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY"}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nLOCAL VERIFICATION SEED BLOCKED — %s\n", err)
		os.Exit(1)
	}
}

// progressf writes seeding progress to stderr, keeping stdout reserved for the
// final manual-test block only.
func progressf(format string, args ...any) { fmt.Fprintf(os.Stderr, format, args...) }

func run() error {
	// Real environment variables win over the file, exactly like main.go.
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	if err := assertLocalDevelopment(cfg); err != nil {
		return err
	}

	progressf("Using database: host=%s port=%d name=%s user=%s sslmode=%s (APP_ENV=%s)\n",
		cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB, cfg.PostgresUser, cfg.PostgresSSLMode, cfg.Env)

	ctx := context.Background()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf(
			"cannot reach the local Postgres at %s:%d/%s — start it first, then re-run. "+
				"This project's local database is a native PostgreSQL on 127.0.0.1:5433 "+
				"(database %q, user %q) as configured in .env; if you use the Docker option instead, run "+
				"`docker compose -f docker-compose.test.yml up -d` and point POSTGRES_PORT at it. Underlying error: %v",
			cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB, cfg.PostgresDB, cfg.PostgresUser, err)
	}
	defer db.Close()

	if err := assertSchemaReady(ctx, db); err != nil {
		return err
	}

	// --- wire the real services, exactly as app.New does -----------------
	users := identityrepository.NewPostgresUserRepository(db)
	sessions := identityrepository.NewPostgresSessionRepository(db)
	hasher := identityservice.NewBcryptHasher()
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{
		AccessLifetime:  cfg.AccessLifetime,
		SessionLifetime: cfg.SessionLifetime,
		PrivateKey:      cfg.PrivateKey,
		PublicKey:       cfg.PublicKey,
	})
	userService := identityservice.NewUserService(users, hasher)
	authService := identityservice.NewAuthenticationService(users, hasher, sessions, tokens)

	tenants := tenantrepository.NewPostgresTenantRepository(db)
	memberships := tenantrepository.NewPostgresMembershipRepository(db)
	tenantService := tenantservice.NewTenantService(db, users, tenants)
	currencyService := tenantservice.NewCurrencyService(tenants)
	onboardingService := tenantservice.NewOnboardingService(tenants)
	publicTenantService := tenantservice.NewPublicTenantService(tenants)

	serviceRepository := schedulingrepository.NewPostgresServiceRepository(db)
	staffRepository := schedulingrepository.NewPostgresStaffRepository(db)
	capabilityRepository := schedulingrepository.NewPostgresCapabilityRepository(db)
	workingHoursRepository := schedulingrepository.NewPostgresWorkingHoursRepository(db)

	catalogService := schedulingservice.NewCatalogService(serviceRepository, tenants)
	staffService := schedulingservice.NewStaffService(db, staffRepository, capabilityRepository, serviceRepository, memberships)
	workingHoursService := schedulingservice.NewWorkingHoursService(db, workingHoursRepository, staffRepository)
	publicCatalogService := schedulingservice.NewPublicCatalogService(publicTenantService, serviceRepository)

	suffix := fmt.Sprintf("%d", time.Now().Unix())
	ownerEmail := fmt.Sprintf("dev-owner-%s@example.test", suffix)

	// --- 1. owner ------------------------------------------------------
	owner, err := userService.Create(ctx, identityservice.CreateUserInput{Email: ownerEmail, Password: devOwnerPassword})
	if err != nil {
		return fmt.Errorf("creating owner user: %w", err)
	}
	progressf("  owner user %s\n", owner.Email)

	// --- 2. Luxe Nails Studio (full public nail tenant) --------------
	luxeSlug := "luxe-nails-" + suffix
	luxe, err := seedPublicTenant(ctx, seedTenantDeps{
		tenantService: tenantService, currencyService: currencyService, onboardingService: onboardingService,
	}, publicTenantInput{
		name: "Luxe Nails Studio", slug: luxeSlug, businessType: string(tenantmodel.BusinessTypeNailTechnician),
		ownerID: owner.ID, timezone: "Africa/Lagos", currency: "NGN",
		description: "Premium gel and nail-care studio in the heart of the city.",
	})
	if err != nil {
		return fmt.Errorf("seeding Luxe Nails Studio: %w", err)
	}

	gel, err := catalogService.Create(ctx, luxe.ID, schedulingservice.CreateServiceInput{
		Name: "Gel Manicure", Description: strptr("Long-lasting gel polish with cuticle care and shaping."),
		DurationMinutes: 45, PriceMinor: 1_500_000, // NGN 15,000.00
	})
	if err != nil {
		return fmt.Errorf("creating Gel Manicure: %w", err)
	}
	pedicure, err := catalogService.Create(ctx, luxe.ID, schedulingservice.CreateServiceInput{
		Name: "Classic Pedicure", Description: strptr("Soak, exfoliation, nail shaping and regular polish."),
		DurationMinutes: 60, PriceMinor: 1_200_000, // NGN 12,000.00
	})
	if err != nil {
		return fmt.Errorf("creating Classic Pedicure: %w", err)
	}

	ada, err := staffService.Create(ctx, luxe.ID, schedulingservice.CreateStaffInput{DisplayName: "Ada Okafor"})
	if err != nil {
		return fmt.Errorf("creating technician Ada Okafor: %w", err)
	}
	bola, err := staffService.Create(ctx, luxe.ID, schedulingservice.CreateStaffInput{DisplayName: "Bola Ade"})
	if err != nil {
		return fmt.Errorf("creating technician Bola Ade: %w", err)
	}

	if _, err := staffService.ReplaceCapabilities(ctx, luxe.ID, ada.ID, []string{gel.ID, pedicure.ID}); err != nil {
		return fmt.Errorf("assigning Ada's capabilities: %w", err)
	}
	if _, err := staffService.ReplaceCapabilities(ctx, luxe.ID, bola.ID, []string{pedicure.ID}); err != nil {
		return fmt.Errorf("assigning Bola's capabilities: %w", err)
	}

	weekly := recurringSchedule()
	if _, err := workingHoursService.ReplaceWeeklySchedule(ctx, luxe.ID, ada.ID, weekly); err != nil {
		return fmt.Errorf("setting Ada's working hours: %w", err)
	}
	if _, err := workingHoursService.ReplaceWeeklySchedule(ctx, luxe.ID, bola.ID, weekly); err != nil {
		return fmt.Errorf("setting Bola's working hours: %w", err)
	}

	// --- 3. Empty Nails (public nail tenant, zero services) --------
	emptySlug := "empty-nails-" + suffix
	if _, err := seedPublicTenant(ctx, seedTenantDeps{
		tenantService: tenantService, currencyService: currencyService, onboardingService: onboardingService,
	}, publicTenantInput{
		name: "Empty Nails", slug: emptySlug, businessType: string(tenantmodel.BusinessTypeNailTechnician),
		ownerID: owner.ID, timezone: "Africa/Lagos", currency: "NGN",
		description: "A brand-new studio still building its service menu.",
	}); err != nil {
		return fmt.Errorf("seeding Empty Nails: %w", err)
	}

	// --- 4. Grand Hotel (HOTEL vertical, no nail services) --------
	hotelSlug := "grand-hotel-" + suffix
	if _, err := seedPublicTenant(ctx, seedTenantDeps{
		tenantService: tenantService, currencyService: currencyService, onboardingService: onboardingService,
	}, publicTenantInput{
		name: "Grand Hotel", slug: hotelSlug, businessType: string(tenantmodel.BusinessTypeHotel),
		ownerID: owner.ID, timezone: "Africa/Lagos", currency: "NGN",
		description: "A full-service hotel — online booking is not offered for this vertical yet.",
	}); err != nil {
		return fmt.Errorf("seeding Grand Hotel: %w", err)
	}

	// --- verification -------------------------------------------------
	v := &verifier{
		auth:          authService,
		publicTenant:  publicTenantService,
		publicCatalog: publicCatalogService,
		staff:         staffService,
		hours:         workingHoursService,
	}
	if err := v.verify(ctx, verifyInput{
		ownerEmail: ownerEmail, ownerPassword: devOwnerPassword,
		luxeSlug: luxeSlug, luxeID: luxe.ID, adaID: ada.ID, bolaID: bola.ID,
		emptySlug: emptySlug, hotelSlug: hotelSlug,
		expectedServices: []string{"Gel Manicure", "Classic Pedicure"},
		adaCapabilities:  2, bolaCapabilities: 1, weeklyIntervals: len(weekly),
	}); err != nil {
		return err
	}

	// --- final output ------------------------------------------------
	base := frontendBaseURL()
	fmt.Println()
	fmt.Println("OWNER LOGIN")
	fmt.Println()
	fmt.Printf("Email: %s\n", ownerEmail)
	fmt.Printf("Password: %s\n", devOwnerPassword)
	fmt.Println()
	fmt.Println("NAIL BOOKING URL")
	fmt.Println()
	fmt.Printf("%s/book/%s\n", base, luxeSlug)
	fmt.Println()
	fmt.Println("EMPTY CATALOG URL")
	fmt.Println()
	fmt.Printf("%s/book/%s\n", base, emptySlug)
	fmt.Println()
	fmt.Println("HOTEL / UNSUPPORTED URL")
	fmt.Println()
	fmt.Printf("%s/book/%s\n", base, hotelSlug)
	fmt.Println()
	fmt.Println("Seed verification data created successfully.")
	return nil
}

// --- production safety ------------------------------------------------

// assertLocalDevelopment fails closed unless every signal says local/dev.
func assertLocalDevelopment(cfg config.Config) error {
	if env := strings.ToLower(strings.TrimSpace(cfg.Env)); env == "production" || env == "prod" || env == "staging" {
		return fmt.Errorf("APP_ENV=%q — this seeder only runs in local development", cfg.Env)
	}

	host := strings.ToLower(strings.TrimSpace(cfg.PostgresHost))
	localHosts := map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true, "0.0.0.0": true, "host.docker.internal": true}
	if !localHosts[host] {
		return fmt.Errorf("POSTGRES_HOST=%q is not a local address — refusing to seed a remote database", cfg.PostgresHost)
	}

	// Belt-and-braces: even a "local" host string must not carry a managed-DB
	// domain, and the SSL mode a hosted database forces is a strong tell.
	managed := []string{"render.com", "amazonaws.com", ".rds.", "neon.tech", "supabase.co", "supabase.com", "database.azure.com", "digitalocean.com", "cockroachlabs.cloud", "planetscale"}
	for _, needle := range managed {
		if strings.Contains(host, needle) {
			return fmt.Errorf("POSTGRES_HOST=%q resembles a managed/production database", cfg.PostgresHost)
		}
	}
	if mode := strings.ToLower(strings.TrimSpace(cfg.PostgresSSLMode)); mode != "disable" && mode != "prefer" && mode != "allow" {
		return fmt.Errorf("POSTGRES_SSLMODE=%q — a local database uses disable/prefer; refusing to run", cfg.PostgresSSLMode)
	}

	if strings.TrimSpace(cfg.PostgresDB) == "" || strings.TrimSpace(cfg.PostgresUser) == "" {
		return fmt.Errorf("local database configuration (POSTGRES_DB / POSTGRES_USER) is missing")
	}
	return nil
}

// assertSchemaReady confirms the migrations this seeder depends on — through
// the S10 bookings table — have been applied to this database.
func assertSchemaReady(ctx context.Context, db *sql.DB) error {
	for _, table := range []string{"users", "tenants", "services", "staff_profiles", "staff_services", "staff_working_hours", "bookings"} {
		var exists bool
		if err := db.QueryRowContext(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)", table,
		).Scan(&exists); err != nil {
			return fmt.Errorf("checking schema: %w", err)
		}
		if !exists {
			return fmt.Errorf("table %q is missing — run `go run ./cmd/migrate` against this database first", table)
		}
	}
	return nil
}

// --- tenant seeding -------------------------------------------------

type seedTenantDeps struct {
	tenantService     tenantservice.TenantService
	currencyService   tenantservice.CurrencyService
	onboardingService tenantservice.OnboardingService
}

type publicTenantInput struct {
	name         string
	slug         string
	businessType string
	ownerID      string
	timezone     string
	currency     string
	description  string
}

// seedPublicTenant runs the exact sequence the product requires for a tenant
// to become publicly visible: create (IN_PROGRESS) → declare currency → set
// the profile timezone → save an onboarding step → complete onboarding. Every
// step goes through the real service, so this cannot drift from what the API
// enforces.
func seedPublicTenant(ctx context.Context, deps seedTenantDeps, in publicTenantInput) (*tenantmodel.Tenant, error) {
	tenant, err := deps.tenantService.Create(ctx, tenantservice.CreateTenantInput{
		Name:          in.name,
		Slug:          in.slug,
		BusinessType:  in.businessType,
		CreatorUserID: in.ownerID,
	})
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}

	if _, err := deps.currencyService.Set(ctx, tenant.ID, in.currency); err != nil {
		return nil, fmt.Errorf("set currency: %w", err)
	}

	description := in.description
	if _, err := deps.tenantService.UpdateProfile(ctx, tenant.ID, tenantservice.UpdateTenantProfileRequest{
		Timezone:    &in.timezone,
		Description: &description,
	}); err != nil {
		return nil, fmt.Errorf("set timezone/profile: %w", err)
	}

	if _, err := deps.onboardingService.SaveProgress(ctx, tenant.ID, tenantservice.SaveOnboardingProgressInput{
		CurrentStep: "business_profile",
	}); err != nil {
		return nil, fmt.Errorf("save onboarding progress: %w", err)
	}

	completed, err := deps.onboardingService.Complete(ctx, tenant.ID)
	if err != nil {
		return nil, fmt.Errorf("complete onboarding: %w", err)
	}
	progressf("  tenant %q -> slug %q (%s, %s)\n", completed.Name, completed.Slug, completed.Status, completed.OnboardingStatus)
	return completed, nil
}

// recurringSchedule is Monday-Saturday, 09:00-12:00 and 14:00-17:00 — a split
// shift with a lunch gap. Recurring weekly (day-of-week), never a date.
func recurringSchedule() []schedulingservice.IntervalInput {
	intervals := make([]schedulingservice.IntervalInput, 0, len(bookingDays)*2)
	for _, day := range bookingDays {
		intervals = append(intervals,
			schedulingservice.IntervalInput{DayOfWeek: day, StartTime: "09:00", EndTime: "12:00"},
			schedulingservice.IntervalInput{DayOfWeek: day, StartTime: "14:00", EndTime: "17:00"},
		)
	}
	return intervals
}

func strptr(s string) *string { return &s }

// frontendBaseURL is the public site origin the /book/{slug} URLs are built on.
// FRONTEND_URL (already in .env) wins; otherwise the App Router dev default.
// It is NEVER derived from a request or from window.location.
func frontendBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("FRONTEND_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:3000"
}

// --- verification -------------------------------------------------

type verifier struct {
	auth          identityservice.AuthenticationService
	publicTenant  tenantservice.PublicTenantService
	publicCatalog schedulingservice.PublicCatalogService
	staff         schedulingservice.StaffService
	hours         schedulingservice.WorkingHoursService
}

type verifyInput struct {
	ownerEmail, ownerPassword         string
	luxeSlug, luxeID, adaID, bolaID   string
	emptySlug, hotelSlug              string
	expectedServices                  []string
	adaCapabilities, bolaCapabilities int
	weeklyIntervals                   int
}

func (v *verifier) verify(ctx context.Context, in verifyInput) error {
	// owner can authenticate through the same service the login API uses.
	auth, err := v.auth.Login(ctx, identityservice.LoginInput{Email: in.ownerEmail, Password: in.ownerPassword})
	if err != nil {
		return fmt.Errorf("owner login failed: %w", err)
	}
	if auth == nil || auth.AccessToken == "" {
		return fmt.Errorf("owner login returned no access token")
	}

	// Luxe Nails public identity.
	luxeIdentity, err := v.publicTenant.GetBySlug(ctx, in.luxeSlug)
	if err != nil {
		return fmt.Errorf("Luxe Nails public identity not resolvable: %w", err)
	}
	if luxeIdentity.BusinessType == nil || *luxeIdentity.BusinessType != tenantmodel.BusinessTypeNailTechnician {
		return fmt.Errorf("Luxe Nails resolved with the wrong business type: %v", luxeIdentity.BusinessType)
	}

	// Luxe Nails public catalog contains the seeded active services.
	luxeCatalog, err := v.publicCatalog.GetCatalog(ctx, in.luxeSlug)
	if err != nil {
		return fmt.Errorf("Luxe Nails public catalog failed: %w", err)
	}
	got := map[string]bool{}
	for _, s := range luxeCatalog.Services {
		got[s.Name] = true
	}
	for _, want := range in.expectedServices {
		if !got[want] {
			return fmt.Errorf("Luxe Nails public catalog missing seeded service %q (got %d services)", want, len(luxeCatalog.Services))
		}
	}
	if len(luxeCatalog.Services) != len(in.expectedServices) {
		return fmt.Errorf("Luxe Nails public catalog has %d services, want %d", len(luxeCatalog.Services), len(in.expectedServices))
	}

	// Empty Nails: 200-equivalent success with zero services.
	emptyCatalog, err := v.publicCatalog.GetCatalog(ctx, in.emptySlug)
	if err != nil {
		return fmt.Errorf("Empty Nails public catalog should succeed with zero services, got: %w", err)
	}
	if len(emptyCatalog.Services) != 0 {
		return fmt.Errorf("Empty Nails public catalog returned %d services, want 0", len(emptyCatalog.Services))
	}

	// Grand Hotel: identity resolves, but the nail catalog is rejected.
	hotelIdentity, err := v.publicTenant.GetBySlug(ctx, in.hotelSlug)
	if err != nil {
		return fmt.Errorf("Grand Hotel public identity not resolvable: %w", err)
	}
	if hotelIdentity.BusinessType == nil || *hotelIdentity.BusinessType != tenantmodel.BusinessTypeHotel {
		return fmt.Errorf("Grand Hotel resolved with the wrong business type: %v", hotelIdentity.BusinessType)
	}
	if _, err := v.publicCatalog.GetCatalog(ctx, in.hotelSlug); err == nil {
		return fmt.Errorf("Grand Hotel public nail catalog should be rejected, but it succeeded")
	} else {
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeResourceNotFound {
			return fmt.Errorf("Grand Hotel public catalog rejected with %v, want RESOURCE_NOT_FOUND", err)
		}
	}

	// Technicians and capabilities persisted.
	adaCaps, err := v.staff.ListCapabilities(ctx, in.luxeID, in.adaID)
	if err != nil {
		return fmt.Errorf("reading Ada's capabilities: %w", err)
	}
	if len(adaCaps) != in.adaCapabilities {
		return fmt.Errorf("Ada has %d capabilities, want %d", len(adaCaps), in.adaCapabilities)
	}
	bolaCaps, err := v.staff.ListCapabilities(ctx, in.luxeID, in.bolaID)
	if err != nil {
		return fmt.Errorf("reading Bola's capabilities: %w", err)
	}
	if len(bolaCaps) != in.bolaCapabilities {
		return fmt.Errorf("Bola has %d capabilities, want %d", len(bolaCaps), in.bolaCapabilities)
	}

	// Working hours persisted.
	adaHours, err := v.hours.List(ctx, in.luxeID, in.adaID)
	if err != nil {
		return fmt.Errorf("reading Ada's working hours: %w", err)
	}
	if len(adaHours) != in.weeklyIntervals {
		return fmt.Errorf("Ada has %d working-hour intervals, want %d", len(adaHours), in.weeklyIntervals)
	}

	progressf("  verification: owner login OK, Luxe identity+catalog OK, Empty catalog OK, Hotel identity OK + catalog rejected, capabilities OK, working hours OK\n")
	return nil
}
