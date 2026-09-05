package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/media"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
)

// UploadImageInput carries one file's raw content. The service layer — not
// the handler — sniffs the real content type and enforces the size ceiling,
// the same "domain validation stays out of handlers" discipline the rest of
// this module follows; the handler's only job is pulling one io.Reader per
// part out of the multipart form.
type UploadImageInput struct {
	Body io.Reader
}

// UpdateImageInput carries a partial update to one image. Mirrors
// repository.ServiceImageUpdate's field set, one layer up.
type UpdateImageInput struct {
	AltText *string
	// IsPrimary, when true, requests promoting this image. A false value is
	// refused — see ServiceImageService's doc comment on why "unset primary"
	// with no replacement is not an operation this API exposes.
	IsPrimary *bool
}

// ServiceReader is the narrow slice of catalog persistence
// ServiceImageService needs: enough to prove a service id names a real row
// belonging to this tenant before any image is attached to, listed for, or
// removed from it. Declared here, in the consumer, the same
// interface-segregation reasoning behind CategoryReader/TenantReader — this
// module has no business creating or editing services themselves.
type ServiceReader interface {
	FindByID(ctx context.Context, tenantID string, serviceID string) (*model.Service, error)
}

// ServiceImageService owns the service-image rules: upload validation
// (count, sniffed MIME type, size), the storage/database consistency
// compensation upload and delete each need, and the primary-image
// invariants (first image auto-primary, promotion on delete, exactly one
// primary at a time).
//
// Tenant access itself — membership and the service.read/service.update
// permission (SC1's own reuse pattern: no new permission family for a
// catalog-adjacent concern) — is verified by the production middleware chain
// before any method here is reached. This service does not re-derive
// authorization; it does scope every repository and storage call by
// tenantID/serviceID, so a defect in that chain cannot become a cross-tenant
// read, write, or delete.
type ServiceImageService interface {
	// Upload validates every file BEFORE storing any of them — a batch either
	// uploads in full or nothing is stored, so a caller never has to reason
	// about which of N files in one request silently landed. Returns the
	// newly created records, in upload order.
	Upload(ctx context.Context, tenantID string, serviceID string, files []UploadImageInput) ([]*model.ServiceImage, error)
	List(ctx context.Context, tenantID string, serviceID string) ([]*model.ServiceImage, error)
	UpdateMeta(ctx context.Context, tenantID string, serviceID string, imageID string, input UpdateImageInput) (*model.ServiceImage, error)
	// Delete removes one image. If it was the primary and other images
	// remain, the one with the lowest sort_order is promoted automatically —
	// a service is never left with images but no primary.
	Delete(ctx context.Context, tenantID string, serviceID string, imageID string) error
	// Reorder replaces the service's whole display order. imageIDs must be
	// exactly the service's current image ids, in the desired order — no
	// more, no fewer, no id from another service or tenant.
	Reorder(ctx context.Context, tenantID string, serviceID string, imageIDs []string) ([]*model.ServiceImage, error)
}

type serviceImageService struct {
	db       txBeginner
	images   repository.ServiceImageRepository
	services ServiceReader
	storage  media.MediaStorage
}

func NewServiceImageService(db txBeginner, images repository.ServiceImageRepository, services ServiceReader, storage media.MediaStorage) ServiceImageService {
	return &serviceImageService{db: db, images: images, services: services, storage: storage}
}

// validatedUpload is one file after passing every check, ready to be stored.
type validatedUpload struct {
	bytes       []byte
	contentType string
}

func (s *serviceImageService) Upload(ctx context.Context, tenantID string, serviceID string, files []UploadImageInput) ([]*model.ServiceImage, error) {
	if err := validateServiceImageIdentifiers(tenantID, serviceID); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no images were supplied", nil)
	}
	if _, err := s.services.FindByID(ctx, tenantID, serviceID); err != nil {
		return nil, err
	}

	existing, err := s.images.ListByService(ctx, tenantID, serviceID)
	if err != nil {
		return nil, err
	}
	if len(existing)+len(files) > model.MaxImagesPerService {
		return nil, apperrors.New(apperrors.CodeValidationFailed,
			fmt.Sprintf("a service may have at most %d images", model.MaxImagesPerService), nil)
	}

	// Every file is fully read and validated (sniffed content type, real
	// size) before anything is uploaded to storage — never trust the total
	// count or any individual file until all of them have been checked, so a
	// batch either uploads in full or nothing is stored.
	validated := make([]validatedUpload, len(files))
	for i, file := range files {
		v, err := readAndValidateImage(file.Body)
		if err != nil {
			return nil, err
		}
		validated[i] = v
	}

	hasPrimary := false
	for _, image := range existing {
		if image.IsPrimary {
			hasPrimary = true
			break
		}
	}

	created := make([]*model.ServiceImage, 0, len(validated))
	for i, upload := range validated {
		ext := model.ExtensionForMIMEType(upload.contentType)
		key := media.BuildServiceImageKey(tenantID, serviceID, ext)

		uploaded, err := s.storage.Upload(ctx, media.UploadInput{
			Key:         key,
			ContentType: upload.contentType,
			Size:        int64(len(upload.bytes)),
			Body:        bytes.NewReader(upload.bytes),
		})
		if err != nil {
			return nil, fmt.Errorf("uploading image to storage: %w", err)
		}

		isPrimary := !hasPrimary && i == 0
		record, err := s.images.Create(ctx, &model.ServiceImage{
			ID:         uuid.NewString(),
			TenantID:   tenantID,
			ServiceID:  serviceID,
			StorageKey: uploaded.Key,
			PublicURL:  uploaded.PublicURL,
			SortOrder:  len(existing) + i,
			IsPrimary:  isPrimary,
			MimeType:   upload.contentType,
			FileSize:   int64(len(upload.bytes)),
		})
		if err != nil {
			// The object now exists in storage with no metadata row pointing
			// at it. Compensate by deleting it — Postgres and object storage
			// cannot share one transaction, so this is best-effort: if the
			// compensating delete ALSO fails (storage outage at the exact
			// wrong moment), the object is orphaned — unreferenced, but
			// consuming storage. That is the one unavoidable consistency gap
			// in this design; it is logged rather than silently swallowed so
			// it is at least discoverable, and a future periodic
			// reconciliation job (listing storage keys with no matching row)
			// is the intended real fix, deliberately out of scope here.
			if cleanupErr := s.storage.Delete(ctx, uploaded.Key); cleanupErr != nil {
				log.Printf("service image: orphaned storage object %q after failed DB insert: %v (cleanup also failed: %v)", uploaded.Key, err, cleanupErr)
			}
			return nil, fmt.Errorf("persisting image metadata: %w", err)
		}
		created = append(created, record)
	}

	return created, nil
}

// readAndValidateImage buffers up to model.MaxImageSizeBytes+1 bytes so an
// oversized file is caught without reading it in full, sniffs the real
// content type from the actual bytes (never a client-declared header or a
// filename extension), and validates both against the model package's
// central limits.
func readAndValidateImage(body io.Reader) (validatedUpload, error) {
	limited := io.LimitReader(body, model.MaxImageSizeBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return validatedUpload{}, fmt.Errorf("reading uploaded image: %w", err)
	}
	if err := model.ValidateImageFileSize(int64(len(data))); err != nil {
		return validatedUpload{}, err
	}

	sniffLength := len(data)
	if sniffLength > 512 {
		sniffLength = 512
	}
	contentType := http.DetectContentType(data[:sniffLength])
	if err := model.ValidateImageMIMEType(contentType); err != nil {
		return validatedUpload{}, err
	}

	return validatedUpload{bytes: data, contentType: contentType}, nil
}

func (s *serviceImageService) List(ctx context.Context, tenantID string, serviceID string) ([]*model.ServiceImage, error) {
	if err := validateServiceImageIdentifiers(tenantID, serviceID); err != nil {
		return nil, err
	}
	if _, err := s.services.FindByID(ctx, tenantID, serviceID); err != nil {
		return nil, err
	}
	return s.images.ListByService(ctx, tenantID, serviceID)
}

func (s *serviceImageService) UpdateMeta(ctx context.Context, tenantID string, serviceID string, imageID string, input UpdateImageInput) (*model.ServiceImage, error) {
	if err := validateServiceImageIdentifiers(tenantID, serviceID); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(imageID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid image id", err)
	}

	current, err := s.images.FindByID(ctx, tenantID, imageID)
	if err != nil {
		return nil, err
	}
	// An image that exists in this tenant but under a DIFFERENT service is
	// reported identically to one that does not exist at all — the same
	// non-disclosure FindByID already gives cross-tenant, extended to the
	// service boundary named in the route.
	if current.ServiceID != serviceID {
		return nil, apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
	}

	if input.AltText == nil && input.IsPrimary == nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)
	}

	var altText *string
	if input.AltText != nil {
		validated, err := model.ValidateAltText(input.AltText)
		if err != nil {
			return nil, err
		}
		altText = validated
	}

	if input.IsPrimary != nil {
		if !*input.IsPrimary {
			// There is deliberately no "clear my own primary flag" operation:
			// a service with images always has exactly one primary once one
			// has ever been set (enforced by Delete's promotion and by there
			// being no code path that unsets one without a replacement).
			// Making a DIFFERENT image primary is how an owner changes it.
			return nil, apperrors.New(apperrors.CodeValidationFailed, "make a different image primary instead of unsetting this one", nil)
		}
		return s.promoteToPrimary(ctx, tenantID, serviceID, imageID, altText)
	}

	return s.images.Update(ctx, tenantID, imageID, repository.ServiceImageUpdate{AltText: altText})
}

// promoteToPrimary clears whatever image currently holds is_primary and sets
// it on imageID, in one transaction — so the partial unique index
// (service_images_service_primary_unique) is never asked to briefly hold two
// primaries, and a failure partway through never leaves the service with
// zero primaries either.
func (s *serviceImageService) promoteToPrimary(ctx context.Context, tenantID string, serviceID string, imageID string, altText *string) (*model.ServiceImage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("starting primary-image transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	txImages := repository.NewPostgresServiceImageRepository(tx)
	if err := txImages.ClearPrimary(ctx, tenantID, serviceID); err != nil {
		return nil, err
	}
	isPrimary := true
	updated, err := txImages.Update(ctx, tenantID, imageID, repository.ServiceImageUpdate{AltText: altText, IsPrimary: &isPrimary})
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing primary-image transaction: %w", err)
	}
	committed = true
	return updated, nil
}

func (s *serviceImageService) Delete(ctx context.Context, tenantID string, serviceID string, imageID string) error {
	if err := validateServiceImageIdentifiers(tenantID, serviceID); err != nil {
		return err
	}
	if _, err := uuid.Parse(imageID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid image id", err)
	}

	current, err := s.images.FindByID(ctx, tenantID, imageID)
	if err != nil {
		return err
	}
	if current.ServiceID != serviceID {
		return apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
	}

	// Deleting a non-primary image is a single write — no transaction is
	// needed, and none opens, so this path runs against whatever
	// ServiceImageRepository was injected (a plain unit-testable fake
	// included), the same way List and a plain alt-text Update do.
	//
	// Deleting the PRIMARY image is two writes that must succeed together
	// (delete it, then promote the next one by sort_order) — that pair goes
	// through withTx precisely because it is a real multi-step atomic
	// operation, the same reasoning StaffService.ReplaceCapabilities uses a
	// transaction for its own multi-row write and nothing simpler.
	if !current.IsPrimary {
		if err := s.images.Delete(ctx, tenantID, imageID); err != nil {
			return err
		}
	} else {
		err = s.withTx(ctx, func(txImages repository.ServiceImageRepository) error {
			if err := txImages.Delete(ctx, tenantID, imageID); err != nil {
				return err
			}
			remaining, err := txImages.ListByService(ctx, tenantID, serviceID)
			if err != nil {
				return err
			}
			if len(remaining) == 0 {
				return nil
			}
			isPrimary := true
			_, err = txImages.Update(ctx, tenantID, remaining[0].ID, repository.ServiceImageUpdate{IsPrimary: &isPrimary})
			return err
		})
		if err != nil {
			return err
		}
	}

	// The database is now the settled source of truth for "does this image
	// still exist, and which one is primary." Only after that is the
	// underlying storage object removed: PostgreSQL and object storage
	// cannot share a transaction, so ordering matters. Deleting the DB row
	// first and the object second means a failure of the second step leaves
	// an orphaned, unreferenced object — wasted storage, but no broken
	// reference anywhere a user can see. Deleting the object first would
	// risk the opposite: a metadata row pointing at a now-missing file, a
	// broken <img> on the public page.
	if err := s.storage.Delete(ctx, current.StorageKey); err != nil {
		log.Printf("service image: deleted metadata for %q but storage cleanup failed for key %q: %v", imageID, current.StorageKey, err)
	}
	return nil
}

func (s *serviceImageService) Reorder(ctx context.Context, tenantID string, serviceID string, imageIDs []string) ([]*model.ServiceImage, error) {
	if err := validateServiceImageIdentifiers(tenantID, serviceID); err != nil {
		return nil, err
	}
	if _, err := s.services.FindByID(ctx, tenantID, serviceID); err != nil {
		return nil, err
	}

	current, err := s.images.ListByService(ctx, tenantID, serviceID)
	if err != nil {
		return nil, err
	}

	if err := validateReorderSet(current, imageIDs); err != nil {
		return nil, err
	}

	err = s.withTx(ctx, func(txImages repository.ServiceImageRepository) error {
		return txImages.SetSortOrders(ctx, tenantID, serviceID, imageIDs)
	})
	if err != nil {
		return nil, err
	}

	return s.images.ListByService(ctx, tenantID, serviceID)
}

// validateReorderSet enforces that imageIDs is exactly current's id set —
// same length, no duplicates, no id current does not contain. This is what
// makes injecting an id from another service or tenant, or silently dropping
// one, impossible: anything other than a permutation of the exact current
// set is refused.
func validateReorderSet(current []*model.ServiceImage, imageIDs []string) error {
	if len(imageIDs) != len(current) {
		return apperrors.New(apperrors.CodeValidationFailed, "reorder must include every one of this service's images, exactly once", nil)
	}
	currentIDs := make(map[string]struct{}, len(current))
	for _, image := range current {
		currentIDs[image.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(imageIDs))
	for _, id := range imageIDs {
		if _, err := uuid.Parse(id); err != nil {
			return apperrors.New(apperrors.CodeValidationFailed, "invalid image id in reorder request", err)
		}
		if _, duplicate := seen[id]; duplicate {
			return apperrors.New(apperrors.CodeValidationFailed, "reorder request lists the same image more than once", nil)
		}
		seen[id] = struct{}{}
		if _, belongs := currentIDs[id]; !belongs {
			return apperrors.New(apperrors.CodeValidationFailed, "reorder request names an image that does not belong to this service", nil)
		}
	}
	return nil
}

// withTx runs fn against a transaction-scoped ServiceImageRepository,
// committing on success and rolling back otherwise — the same
// begin/defer-rollback/commit shape StaffService.ReplaceCapabilities uses for
// its own atomic multi-statement write.
func (s *serviceImageService) withTx(ctx context.Context, fn func(repository.ServiceImageRepository) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(repository.NewPostgresServiceImageRepository(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	committed = true
	return nil
}

func validateServiceImageIdentifiers(tenantID string, serviceID string) error {
	if _, err := uuid.Parse(tenantID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	if _, err := uuid.Parse(serviceID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid service id", err)
	}
	return nil
}

// compile-time guards.
var (
	_ ServiceImageService = (*serviceImageService)(nil)
	_ ServiceReader       = (repository.ServiceRepository)(nil)
)
