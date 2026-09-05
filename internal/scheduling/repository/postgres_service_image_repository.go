package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

type PostgresServiceImageRepository struct{ db dbtx }

func NewPostgresServiceImageRepository(db dbtx) *PostgresServiceImageRepository {
	return &PostgresServiceImageRepository{db: db}
}

// imageColumns is the full column set every SELECT below reads, kept as one
// constant so the query strings and scanImage's argument order cannot
// silently drift apart — the same discipline serviceColumns/categoryColumns
// already apply.
const imageColumns = "id, tenant_id, service_id, storage_key, public_url, alt_text, sort_order, is_primary, mime_type, file_size, created_at, updated_at"

func scanImage(row scanner) (*model.ServiceImage, error) {
	var image model.ServiceImage
	err := row.Scan(
		&image.ID, &image.TenantID, &image.ServiceID, &image.StorageKey, &image.PublicURL,
		&image.AltText, &image.SortOrder, &image.IsPrimary, &image.MimeType, &image.FileSize,
		&image.CreatedAt, &image.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// imageNotFound is the single response to "no such image in this tenant". It
// is used identically for an image that does not exist and one that exists
// under a different tenant, the same reasoning notFound/categoryNotFound
// already apply.
func imageNotFound(cause error) error {
	return apperrors.New(apperrors.CodeImageNotFound, "service image not found", cause)
}

func (r *PostgresServiceImageRepository) Create(ctx context.Context, image *model.ServiceImage) (*model.ServiceImage, error) {
	created := *image
	const query = `INSERT INTO service_images (id, tenant_id, service_id, storage_key, public_url, alt_text, sort_order, is_primary, mime_type, file_size)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query,
		image.ID, image.TenantID, image.ServiceID, image.StorageKey, image.PublicURL,
		image.AltText, image.SortOrder, image.IsPrimary, image.MimeType, image.FileSize,
	).Scan(&created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("inserting service image: %w", err)
	}
	return &created, nil
}

func (r *PostgresServiceImageRepository) FindByID(ctx context.Context, tenantID string, imageID string) (*model.ServiceImage, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+imageColumns+" FROM service_images WHERE id = $1 AND tenant_id = $2", imageID, tenantID)
	image, err := scanImage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, imageNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding service image: %w", err)
	}
	return image, nil
}

// ListByService returns the tenant-scoped service's images ordered by
// sort_order, then id — a stable tiebreak between images inserted with the
// same order value (every image starts at sort_order 0 until a reorder ever
// runs).
func (r *PostgresServiceImageRepository) ListByService(ctx context.Context, tenantID string, serviceID string) ([]*model.ServiceImage, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT "+imageColumns+" FROM service_images WHERE tenant_id = $1 AND service_id = $2 ORDER BY sort_order ASC, id ASC",
		tenantID, serviceID)
	if err != nil {
		return nil, fmt.Errorf("listing service images: %w", err)
	}
	defer rows.Close()
	var images []*model.ServiceImage
	for rows.Next() {
		image, err := scanImage(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning service image: %w", err)
		}
		images = append(images, image)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating service images: %w", err)
	}
	return images, nil
}

// ListByServiceIDs returns every image for every named service, ordered so
// that images for the same service are contiguous and each service's own
// images are already in display order — exactly the shape a caller needs to
// group them by ServiceID in one pass with no further sorting.
func (r *PostgresServiceImageRepository) ListByServiceIDs(ctx context.Context, tenantID string, serviceIDs []string) ([]*model.ServiceImage, error) {
	if len(serviceIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(serviceIDs))
	args := make([]any, 0, len(serviceIDs)+1)
	args = append(args, tenantID)
	for i, id := range serviceIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, id)
	}
	query := "SELECT " + imageColumns + " FROM service_images WHERE tenant_id = $1 AND service_id IN (" +
		strings.Join(placeholders, ", ") + ") ORDER BY service_id, sort_order ASC, id ASC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing service images for services: %w", err)
	}
	defer rows.Close()
	var images []*model.ServiceImage
	for rows.Next() {
		image, err := scanImage(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning service image: %w", err)
		}
		images = append(images, image)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating service images: %w", err)
	}
	return images, nil
}

func (r *PostgresServiceImageRepository) Update(ctx context.Context, tenantID string, imageID string, update ServiceImageUpdate) (*model.ServiceImage, error) {
	sets := []string{}
	args := []any{}
	argIndex := 1

	if update.AltText != nil {
		sets = append(sets, fmt.Sprintf("alt_text = $%d", argIndex))
		args = append(args, *update.AltText)
		argIndex++
	}
	if update.IsPrimary != nil {
		sets = append(sets, fmt.Sprintf("is_primary = $%d", argIndex))
		args = append(args, *update.IsPrimary)
		argIndex++
	}
	if len(sets) == 0 {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)
	}

	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, imageID, tenantID)

	query := fmt.Sprintf(
		"UPDATE service_images SET %s WHERE id = $%d AND tenant_id = $%d RETURNING %s",
		strings.Join(sets, ", "), argIndex, argIndex+1, imageColumns,
	)

	row := r.db.QueryRowContext(ctx, query, args...)
	image, err := scanImage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, imageNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("updating service image: %w", err)
	}
	return image, nil
}

func (r *PostgresServiceImageRepository) ClearPrimary(ctx context.Context, tenantID string, serviceID string) error {
	if _, err := r.db.ExecContext(ctx,
		"UPDATE service_images SET is_primary = FALSE, updated_at = CURRENT_TIMESTAMP WHERE tenant_id = $1 AND service_id = $2 AND is_primary = TRUE",
		tenantID, serviceID,
	); err != nil {
		return fmt.Errorf("clearing primary service image: %w", err)
	}
	return nil
}

func (r *PostgresServiceImageRepository) Delete(ctx context.Context, tenantID string, imageID string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM service_images WHERE id = $1 AND tenant_id = $2", imageID, tenantID)
	if err != nil {
		return fmt.Errorf("deleting service image: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking delete result: %w", err)
	}
	if affected == 0 {
		return imageNotFound(nil)
	}
	return nil
}

// SetSortOrders writes 0..N-1 as each image's new sort_order, in the given
// order. The caller has already validated orderedImageIDs is exactly the
// service's current image set (see ServiceImageService.Reorder) — this
// method does not re-derive that, only applies it, mirroring how
// PostgresCapabilityRepository.Assign trusts its caller's validation and
// leans on the schema (here: tenant_id in the WHERE clause) as the backstop.
func (r *PostgresServiceImageRepository) SetSortOrders(ctx context.Context, tenantID string, serviceID string, orderedImageIDs []string) error {
	for index, imageID := range orderedImageIDs {
		result, err := r.db.ExecContext(ctx,
			"UPDATE service_images SET sort_order = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND tenant_id = $3 AND service_id = $4",
			index, imageID, tenantID, serviceID,
		)
		if err != nil {
			return fmt.Errorf("setting sort order for image %s: %w", imageID, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking reorder result for image %s: %w", imageID, err)
		}
		if affected == 0 {
			return imageNotFound(nil)
		}
	}
	return nil
}

// compile-time guard: the Postgres implementation must keep satisfying the
// repository interface.
var _ ServiceImageRepository = (*PostgresServiceImageRepository)(nil)
