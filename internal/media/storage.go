// Package media is the storage abstraction for service images (and any future
// tenant-uploaded binary asset). It exists so the scheduling module never
// talks to a specific cloud vendor directly — only to the MediaStorage
// interface below — which is what keeps a provider swap (local disk today,
// S3/Cloudinary/R2 tomorrow) a one-file change instead of a scheduling-module
// rewrite.
//
// Nothing in this package, or anything built on it, ever persists image
// bytes to PostgreSQL. Postgres holds metadata only (see
// internal/scheduling/model/service_image.go); the bytes themselves live
// wherever the configured MediaStorage implementation puts them.
package media

import (
	"context"
	"io"
)

// UploadInput is everything a MediaStorage implementation needs to accept one
// file. Key is server-computed (see BuildServiceImageKey) — never the
// caller-supplied filename — so a storage implementation never has to defend
// itself against a hostile path.
type UploadInput struct {
	// Key is the full logical storage path, e.g.
	// "tenants/{tenantID}/services/{serviceID}/{uuid}.webp". Implementations
	// must treat it as an opaque, already-safe string.
	Key string
	// ContentType is the sniffed (never trusted-from-header-alone) MIME type.
	ContentType string
	// Size is the exact byte length of Body, known up front so an
	// implementation can allocate or validate without buffering twice.
	Size int64
	Body io.Reader
}

// UploadedFile is what a successful upload returns: enough for the caller to
// persist a service_images row and render an <img> tag. Nothing here ever
// includes a credential, a signed-request secret, or a filesystem path
// outside the storage root.
type UploadedFile struct {
	// Key echoes UploadInput.Key — the caller needs it back to be able to
	// issue Delete later without having tracked it separately.
	Key string
	// PublicURL is a fully-qualified, directly-fetchable URL. It is safe to
	// return to an anonymous customer on the public booking page.
	PublicURL string
}

// MediaStorage is the one seam the scheduling module depends on. It is
// deliberately narrow — upload and delete, nothing else (no listing, no
// signed-URL generation, no bucket administration) — because that is the
// entire surface a service image's lifecycle needs.
type MediaStorage interface {
	Upload(ctx context.Context, input UploadInput) (*UploadedFile, error)
	// Delete removes the object named by key. Deleting a key that does not
	// exist is not an error — the caller (ServiceImageService) treats
	// "already gone" and "successfully removed" identically, since both
	// leave the system in the same state.
	Delete(ctx context.Context, key string) error
}
