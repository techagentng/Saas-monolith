package model

import (
	"strings"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

// Upload limits, centralized here rather than scattered across the handler,
// service and repository layers — the same discipline MaxNameLength/
// MaxDurationMinutes/MaxPriceMinor already establish for the rest of the
// catalog. Every one of these is a policy decision a future product change
// might revisit; changing it here changes it everywhere it is enforced.
const (
	// MaxImagesPerService bounds how many images one service may carry at
	// once. Enforced server-side in ServiceImageService — a client-side
	// count is a UX convenience only, never trusted.
	MaxImagesPerService = 5
	// MaxImageSizeBytes bounds a single upload. 5 MB comfortably fits a
	// phone photo without needing chunked upload machinery this feature does
	// not build.
	MaxImageSizeBytes int64 = 5 * 1024 * 1024
	// MaxAltTextLength mirrors the service_images.alt_text column width
	// (migration 000020).
	MaxAltTextLength = 255
)

// AllowedImageMIMETypes is the exhaustive, initial set of content types a
// service image may be. SVG is deliberately absent — an SVG can embed
// script, and this feature explicitly does not attempt to sanitize one, so
// the simplest safe answer is to never accept it. Membership must be
// resolved against the SNIFFED content type (see handler's use of
// http.DetectContentType), never a client-declared Content-Type header or a
// filename extension — both are attacker-controlled and prove nothing about
// the actual bytes.
var AllowedImageMIMETypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// ServiceImage is one uploaded photo attached to a service.
//
// It carries no currency, no status/archive lifecycle, and no ordering
// authority beyond SortOrder — an image is either present or deleted, there
// is no intermediate "archived" state the way a Service or ServiceCategory
// has. Deletion here really does delete the row (and, via
// ServiceImageService, the underlying object): unlike a service or category,
// nothing else in the system ever holds a foreign key to an image, so there
// is no historical-reference reason to keep a "removed" row around.
type ServiceImage struct {
	ID       string
	TenantID string
	// ServiceID is not itself re-validated against TenantID here — the
	// composite foreign key in migration 000020
	// (service_images_service_tenant_fkey) is what makes a cross-tenant
	// pairing impossible at the schema level, the identical guarantee
	// services.category_id already relies on for categories.
	ServiceID string
	// StorageKey is the MediaStorage handle (see internal/media), always
	// server-generated (media.BuildServiceImageKey) — never derived from a
	// client-supplied filename or path.
	StorageKey string
	// PublicURL is the fully-qualified address the public booking page and
	// the owner dashboard both render directly.
	PublicURL string
	// AltText is nil when the owner never supplied one; a caller rendering
	// the public catalogue falls back to the service's own name in that case
	// (see the public handler), not to an empty string.
	AltText   *string
	SortOrder int
	// IsPrimary is true for at most one image per service — enforced by the
	// partial unique index service_images_service_primary_unique, so a
	// defect here surfaces as a database error rather than silent duplicate
	// primaries.
	IsPrimary bool
	// MimeType is the sniffed content type at upload time, one of
	// AllowedImageMIMETypes' keys — re-validated by the
	// service_images_mime_type_valid CHECK regardless of what wrote the row.
	MimeType  string
	FileSize  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ValidateAltText trims and bounds an optional caption. Mirrors
// ValidateDescription's nil/empty-string distinction: a nil input stays nil
// ("never set"); a supplied value that trims to empty is kept as an empty
// string rather than folded back to nil, so "cleared to empty" and "never
// set" remain distinguishable to a caller that cares.
func ValidateAltText(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	altText := strings.TrimSpace(*value)
	if len(altText) > MaxAltTextLength {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "image alt text exceeds maximum length", nil)
	}
	return &altText, nil
}

// ValidateImageMIMEType checks a sniffed content type against the allow-list.
// The caller must pass the result of sniffing the actual bytes
// (http.DetectContentType), never a client-declared header or a filename
// extension — this function has no way to tell the difference, which is
// exactly why that discipline belongs to the caller, not here.
func ValidateImageMIMEType(mimeType string) error {
	if _, ok := AllowedImageMIMETypes[mimeType]; !ok {
		return apperrors.New(apperrors.CodeValidationFailed, "unsupported image type: only JPEG, PNG and WebP are allowed", nil)
	}
	return nil
}

// ValidateImageFileSize bounds one upload's byte size. Zero and negative are
// refused alongside anything over the ceiling — a zero-byte "image" is not a
// real upload, and reaching this with a negative size would mean the caller
// never actually measured the body.
func ValidateImageFileSize(size int64) error {
	if size <= 0 {
		return apperrors.New(apperrors.CodeValidationFailed, "image file is empty", nil)
	}
	if size > MaxImageSizeBytes {
		return apperrors.New(apperrors.CodeValidationFailed, "image exceeds the maximum file size", nil)
	}
	return nil
}

// ExtensionForMIMEType returns the storage-key file extension for an already
// validated MIME type. Callers must run ValidateImageMIMEType first — this
// returns "" for anything not in AllowedImageMIMETypes, which
// media.BuildServiceImageKey would turn into an extension-less key rather
// than panicking, but no code path should ever reach it with an
// unvalidated type.
func ExtensionForMIMEType(mimeType string) string {
	return AllowedImageMIMETypes[mimeType]
}
