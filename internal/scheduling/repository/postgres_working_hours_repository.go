package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

type PostgresWorkingHoursRepository struct{ db dbtx }

func NewPostgresWorkingHoursRepository(db dbtx) *PostgresWorkingHoursRepository {
	return &PostgresWorkingHoursRepository{db: db}
}

const workingHoursColumns = "id, tenant_id, staff_id, day_of_week, start_time, end_time, created_at, updated_at"

// microsecondsPerMinute mirrors the unit pgtype.Time stores internally
// (microseconds since midnight). The domain layer only ever produces
// HH:MM — no seconds, no fractional minute — so the conversion in both
// directions is exact and lossless.
const microsecondsPerMinute = int64(60_000_000)

// clockTimeToPGTime converts the model's validated "HH:MM" representation
// into the one Go type pgx v5 actually supports for a TIME column:
// pgtype.Time is the type the library authors built for this exact purpose —
// it implements both driver.Valuer (for writing) and sql.Scanner (for
// reading) — where a bare string or time.Time is not a reliable encoding
// target for TIME across pgx's binary and text protocols. The caller is
// expected to have already validated the input via model.ValidateClockTime;
// a genuinely malformed value here is a programmer error, not a client
// input, and is reported as an internal wrapped error rather than a
// presentable one.
func clockTimeToPGTime(value string) (pgtype.Time, error) {
	if len(value) != 5 || value[2] != ':' {
		return pgtype.Time{}, fmt.Errorf("working hours repository: %q is not a validated HH:MM value", value)
	}
	var hours, minutes int
	if _, err := fmt.Sscanf(value, "%02d:%02d", &hours, &minutes); err != nil {
		return pgtype.Time{}, fmt.Errorf("working hours repository: parsing %q: %w", value, err)
	}
	return pgtype.Time{Microseconds: (int64(hours)*60 + int64(minutes)) * microsecondsPerMinute, Valid: true}, nil
}

// pgTimeToClockTime is clockTimeToPGTime's inverse, used when scanning a row
// back out of the database.
func pgTimeToClockTime(value pgtype.Time) string {
	totalMinutes := value.Microseconds / microsecondsPerMinute
	return fmt.Sprintf("%02d:%02d", totalMinutes/60, totalMinutes%60)
}

func scanWorkingHourInterval(row scanner) (*model.WorkingHourInterval, error) {
	var (
		interval   model.WorkingHourInterval
		start, end pgtype.Time
	)
	err := row.Scan(
		&interval.ID, &interval.TenantID, &interval.StaffID, &interval.DayOfWeek,
		&start, &end, &interval.CreatedAt, &interval.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	interval.StartTime = pgTimeToClockTime(start)
	interval.EndTime = pgTimeToClockTime(end)
	return &interval, nil
}

// ListByStaff orders by day of week (Monday first, via an explicit CASE —
// day_of_week is stored as text, which sorts alphabetically otherwise) then
// start time, matching the ordering model.ValidateWeeklySchedule already
// applies to a write, so a read and a just-written schedule agree.
func (r *PostgresWorkingHoursRepository) ListByStaff(ctx context.Context, tenantID string, staffID string) ([]*model.WorkingHourInterval, error) {
	const query = `SELECT ` + workingHoursColumns + ` FROM staff_working_hours
        WHERE tenant_id = $1 AND staff_id = $2
        ORDER BY CASE day_of_week
            WHEN 'MONDAY' THEN 1 WHEN 'TUESDAY' THEN 2 WHEN 'WEDNESDAY' THEN 3
            WHEN 'THURSDAY' THEN 4 WHEN 'FRIDAY' THEN 5 WHEN 'SATURDAY' THEN 6
            WHEN 'SUNDAY' THEN 7 END, start_time ASC`
	rows, err := r.db.QueryContext(ctx, query, tenantID, staffID)
	if err != nil {
		return nil, fmt.Errorf("listing working hours: %w", err)
	}
	defer rows.Close()
	intervals := []*model.WorkingHourInterval{}
	for rows.Next() {
		interval, err := scanWorkingHourInterval(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning working hour interval: %w", err)
		}
		intervals = append(intervals, interval)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating working hours: %w", err)
	}
	return intervals, nil
}

func (r *PostgresWorkingHoursRepository) DeleteAllForStaff(ctx context.Context, tenantID string, staffID string) error {
	if _, err := r.db.ExecContext(ctx,
		"DELETE FROM staff_working_hours WHERE tenant_id = $1 AND staff_id = $2", tenantID, staffID,
	); err != nil {
		return fmt.Errorf("clearing working hours: %w", err)
	}
	return nil
}

// Create inserts one interval. A composite-foreign-key violation means the
// staff member does not belong to the named tenant — the service layer
// validates this beforehand, so reaching it here is either a race or a
// caller bypassing that layer, reported as a presentable validation failure
// rather than a 500, matching CapabilityRepository.Assign's own reasoning
// for the identical situation on staff_services.
func (r *PostgresWorkingHoursRepository) Create(ctx context.Context, interval *model.WorkingHourInterval) (*model.WorkingHourInterval, error) {
	start, err := clockTimeToPGTime(interval.StartTime)
	if err != nil {
		return nil, err
	}
	end, err := clockTimeToPGTime(interval.EndTime)
	if err != nil {
		return nil, err
	}
	const query = `INSERT INTO staff_working_hours (id, tenant_id, staff_id, day_of_week, start_time, end_time)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING created_at, updated_at`
	created := *interval
	err = r.db.QueryRowContext(ctx, query,
		interval.ID, interval.TenantID, interval.StaffID, string(interval.DayOfWeek), start, end,
	).Scan(&created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		if isWorkingHoursStaffTenantViolation(err) {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "staff member does not belong to this tenant", err)
		}
		if isWorkingHoursDuplicateIntervalViolation(err) {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "duplicate working hour interval", err)
		}
		return nil, fmt.Errorf("inserting working hour interval: %w", err)
	}
	return &created, nil
}

// isWorkingHoursStaffTenantViolation recognizes an insert naming a staff
// member that does not belong to the given tenant — the composite foreign
// key staff_working_hours_staff_tenant_fkey firing. Matched by constraint
// name rather than any 23503, the same discipline
// isMissingUserViolation applies in the staff repository: a different
// foreign-key violation added later must not be silently reported as this
// one.
func isWorkingHoursStaffTenantViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "staff_working_hours_staff_tenant_fkey"
}

// isWorkingHoursDuplicateIntervalViolation recognizes the exact-duplicate
// backstop, staff_working_hours_unique_interval, firing. Genuine partial
// overlap is never caught here — it is not expressible as a UNIQUE
// constraint — and is rejected earlier, by
// model.ValidateWeeklySchedule, before this insert is ever reached.
func isWorkingHoursDuplicateIntervalViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "staff_working_hours_unique_interval"
}

var _ WorkingHoursRepository = (*PostgresWorkingHoursRepository)(nil)
