package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

// bookingColumns is the full column set every plain-booking SELECT/RETURNING
// reads, kept as one constant so query strings and scanBooking cannot drift.
const bookingColumns = "id, tenant_id, service_id, staff_id, customer_name, customer_phone, customer_email, start_at, end_at, status, created_at, updated_at"

func scanBooking(row scanner) (*model.Booking, error) {
	var booking model.Booking
	err := row.Scan(
		&booking.ID, &booking.TenantID, &booking.ServiceID, &booking.StaffID,
		&booking.Customer.Name, &booking.Customer.Phone, &booking.Customer.Email,
		&booking.StartAt, &booking.EndAt, &booking.Status,
		&booking.CreatedAt, &booking.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func scanBookingWithRelations(row scanner) (*BookingWithRelations, error) {
	var out BookingWithRelations
	err := row.Scan(
		&out.Booking.ID, &out.Booking.TenantID, &out.Booking.ServiceID, &out.Booking.StaffID,
		&out.Booking.Customer.Name, &out.Booking.Customer.Phone, &out.Booking.Customer.Email,
		&out.Booking.StartAt, &out.Booking.EndAt, &out.Booking.Status,
		&out.Booking.CreatedAt, &out.Booking.UpdatedAt,
		&out.ServiceName, &out.ServiceDurationMins, &out.StaffName,
	)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// bookingRelationSelect is the list/detail projection: every booking column
// (prefixed b.) plus the joined display fields. The joins are on the composite
// (id, tenant_id) of both parents, so a booking can only ever join to a service
// and staff profile in its own tenant.
const bookingRelationSelect = `
    SELECT b.id, b.tenant_id, b.service_id, b.staff_id,
           b.customer_name, b.customer_phone, b.customer_email,
           b.start_at, b.end_at, b.status, b.created_at, b.updated_at,
           s.name, s.duration_minutes, st.display_name
    FROM bookings b
    JOIN services s ON s.id = b.service_id AND s.tenant_id = b.tenant_id
    JOIN staff_profiles st ON st.id = b.staff_id AND st.tenant_id = b.tenant_id`

// PostgresBookingRepository persists appointment bookings and answers the S7
// occupancy query. It also is the production OccupancyReader — its
// OccupiedIntervals signature matches that interface exactly, so app.New wires
// this one object in place of NoOccupancy.
type PostgresBookingRepository struct{ db dbtx }

func NewPostgresBookingRepository(db dbtx) *PostgresBookingRepository {
	return &PostgresBookingRepository{db: db}
}

// Create inserts one booking.
//
// Two constraint failures are expected and mapped, never surfaced raw:
//
//   - bookings_no_overlap (SQLSTATE 23P01): another CONFIRMED booking already
//     holds — or, under concurrency, just committed into — an overlapping
//     interval for this staff member. This is the deterministic loser of a
//     race; it becomes BOOKING_SLOT_UNAVAILABLE (409), not a 500.
//   - a composite FK (23503): the service or staff id does not belong to this
//     tenant. The service layer validates this first, so reaching it is a race
//     or a bypass; reported as a presentable validation failure, matching how
//     the working-hours repository handles the identical situation.
func (r *PostgresBookingRepository) Create(ctx context.Context, booking *model.Booking) (*model.Booking, error) {
	const query = `INSERT INTO bookings
        (id, tenant_id, service_id, staff_id, customer_name, customer_phone, customer_email, start_at, end_at, status)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
        RETURNING created_at, updated_at`

	status := booking.Status
	if status == "" {
		status = model.BookingConfirmed
	}
	created := *booking
	created.Status = status

	err := r.db.QueryRowContext(ctx, query,
		booking.ID, booking.TenantID, booking.ServiceID, booking.StaffID,
		booking.Customer.Name, booking.Customer.Phone, booking.Customer.Email,
		booking.StartAt.UTC(), booking.EndAt.UTC(), string(status),
	).Scan(&created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		if isBookingOverlapViolation(err) {
			return nil, apperrors.New(apperrors.CodeBookingSlotUnavailable, "the requested time is no longer available", err)
		}
		if isBookingTenantScopeViolation(err) {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "service or staff member does not belong to this tenant", err)
		}
		return nil, fmt.Errorf("inserting booking: %w", err)
	}
	return &created, nil
}

// OccupiedIntervals returns this staff member's CONFIRMED bookings that
// intersect [from, to). The predicate is the half-open overlap rule the S7
// engine uses — start_at < to AND end_at > from — and it is served by
// bookings_tenant_staff_start_idx rather than by scanning the tenant's
// bookings.
func (r *PostgresBookingRepository) OccupiedIntervals(ctx context.Context, tenantID string, staffID string, from time.Time, to time.Time) ([]availability.OccupiedInterval, error) {
	const query = `SELECT start_at, end_at FROM bookings
        WHERE tenant_id = $1 AND staff_id = $2 AND status = 'CONFIRMED'
          AND start_at < $3 AND end_at > $4
        ORDER BY start_at ASC`
	rows, err := r.db.QueryContext(ctx, query, tenantID, staffID, to.UTC(), from.UTC())
	if err != nil {
		return nil, fmt.Errorf("listing occupied intervals: %w", err)
	}
	defer rows.Close()

	intervals := []availability.OccupiedInterval{}
	for rows.Next() {
		var start, end time.Time
		if err := rows.Scan(&start, &end); err != nil {
			return nil, fmt.Errorf("scanning occupied interval: %w", err)
		}
		intervals = append(intervals, availability.OccupiedInterval{Start: start, End: end})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating occupied intervals: %w", err)
	}
	return intervals, nil
}

// ListByTenant builds the filtered list query. Every predicate is a bound
// parameter appended in a fixed order; nothing is string-interpolated. The
// result is served by bookings_tenant_status_start_idx / bookings_tenant_start_idx.
func (r *PostgresBookingRepository) ListByTenant(ctx context.Context, tenantID string, filter BookingListFilter) ([]*BookingWithRelations, error) {
	conditions := []string{"b.tenant_id = $1"}
	args := []any{tenantID}

	if filter.Status != nil {
		args = append(args, string(*filter.Status))
		conditions = append(conditions, fmt.Sprintf("b.status = $%d", len(args)))
	}
	switch filter.Window {
	case BookingWindowUpcoming:
		args = append(args, filter.Now.UTC())
		conditions = append(conditions, fmt.Sprintf("b.start_at >= $%d", len(args)))
	case BookingWindowPast:
		args = append(args, filter.Now.UTC())
		conditions = append(conditions, fmt.Sprintf("b.start_at < $%d", len(args)))
	}
	if filter.StaffID != nil {
		args = append(args, *filter.StaffID)
		conditions = append(conditions, fmt.Sprintf("b.staff_id = $%d", len(args)))
	}
	if filter.ServiceID != nil {
		args = append(args, *filter.ServiceID)
		conditions = append(conditions, fmt.Sprintf("b.service_id = $%d", len(args)))
	}

	query := bookingRelationSelect + "\n    WHERE " + strings.Join(conditions, " AND ") + "\n    ORDER BY b.start_at ASC, b.id ASC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing bookings: %w", err)
	}
	defer rows.Close()

	result := []*BookingWithRelations{}
	for rows.Next() {
		item, err := scanBookingWithRelations(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning booking: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating bookings: %w", err)
	}
	return result, nil
}

// FindByTenantAndID resolves one booking within one tenant. A row that does
// not exist, or belongs to another tenant, yields BOOKING_NOT_FOUND — the two
// are deliberately indistinguishable.
func (r *PostgresBookingRepository) FindByTenantAndID(ctx context.Context, tenantID string, bookingID string) (*BookingWithRelations, error) {
	row := r.db.QueryRowContext(ctx, bookingRelationSelect+"\n    WHERE b.tenant_id = $1 AND b.id = $2", tenantID, bookingID)
	item, err := scanBookingWithRelations(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeBookingNotFound, "booking not found", nil)
	}
	if err != nil {
		return nil, fmt.Errorf("finding booking: %w", err)
	}
	return item, nil
}

// Cancel flips a CONFIRMED booking to CANCELLED in place, scoped by both id and
// tenant_id. It returns updated=false (and a nil booking) when no CONFIRMED row
// matched — the service turns that into either BOOKING_NOT_FOUND or an
// idempotent success after checking the current state. The row is never
// deleted, so booking history is preserved.
func (r *PostgresBookingRepository) Cancel(ctx context.Context, tenantID string, bookingID string) (*model.Booking, bool, error) {
	const query = `UPDATE bookings
        SET status = 'CANCELLED', updated_at = CURRENT_TIMESTAMP
        WHERE id = $1 AND tenant_id = $2 AND status = 'CONFIRMED'
        RETURNING ` + bookingColumns
	booking, err := scanBooking(r.db.QueryRowContext(ctx, query, bookingID, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cancelling booking: %w", err)
	}
	return booking, true, nil
}

// isBookingOverlapViolation recognizes the no-overlap exclusion constraint
// firing. Matched by SQLSTATE 23P01 (exclusion_violation) plus the constraint
// name, so a future exclusion constraint on this table cannot be silently
// reported as a slot conflict — the same discipline the working-hours
// repository applies to its own constraints.
func isBookingOverlapViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23P01" && pgErr.ConstraintName == "bookings_no_overlap"
}

// isBookingTenantScopeViolation recognizes either composite foreign key
// (service or staff not in the booking's tenant) firing.
func isBookingTenantScopeViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		return false
	}
	return pgErr.ConstraintName == "bookings_service_tenant_fkey" || pgErr.ConstraintName == "bookings_staff_tenant_fkey"
}

// compile-time guards.
var _ BookingRepository = (*PostgresBookingRepository)(nil)
