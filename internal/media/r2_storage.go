package media

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
)

// r2API is the narrow slice of *s3.Client that R2Storage uses. Depending on an
// interface rather than the concrete client is what lets the tests substitute
// an in-memory fake — no unit test ever reaches real Cloudflare R2.
type r2API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// R2Storage implements MediaStorage against a Cloudflare R2 bucket (which is
// S3-compatible) using the AWS SDK for Go v2.
//
// Storage-key semantics match the local driver exactly: the logical key
// (UploadInput.Key, e.g. "tenants/{t}/services/{s}/{uuid}.webp") is what is
// returned and therefore what service_images.storage_key persists. The
// configured prefix ("serviceimg") is a physical detail applied only when
// talking to R2 — objectKey() adds it on the way in, Delete re-adds it on the
// way out — so existing rows written by the local driver stay valid and a
// future driver swap does not require a data migration.
type R2Storage struct {
	client        r2API
	bucket        string
	prefix        string
	publicBaseURL string
}

// NewR2Storage validates its inputs and normalizes the prefix (no leading or
// trailing slash) and the public base URL (no trailing slash).
func NewR2Storage(client r2API, bucket, prefix, publicBaseURL string) (*R2Storage, error) {
	if client == nil || isNilR2API(client) {
		return nil, errors.New("media: R2 storage requires a non-nil S3 client")
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return nil, errors.New("media: R2 storage requires a bucket")
	}
	publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if publicBaseURL == "" {
		return nil, errors.New("media: R2 storage requires a public base URL")
	}
	return &R2Storage{
		client:        client,
		bucket:        bucket,
		prefix:        normalizeStoragePrefix(prefix),
		publicBaseURL: publicBaseURL,
	}, nil
}

// normalizeStoragePrefix trims surrounding whitespace and every leading and
// trailing slash, so the prefix joins with a key exactly once. "" is a valid
// result: an empty prefix means objects are written at the bucket root.
func normalizeStoragePrefix(prefix string) string {
	return strings.Trim(strings.TrimSpace(prefix), "/")
}

// objectKey turns a logical storage key into the physical R2 object key by
// applying the configured prefix exactly once. Leading slashes on the logical
// key are stripped so the result can never contain "//" or escape the prefix.
func (s *R2Storage) objectKey(logicalKey string) string {
	key := strings.TrimLeft(logicalKey, "/")
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func (s *R2Storage) Upload(ctx context.Context, input UploadInput) (*UploadedFile, error) {
	logicalKey := strings.TrimSpace(input.Key)
	if logicalKey == "" {
		return nil, errors.New("media: upload requires a key")
	}
	objectKey := s.objectKey(logicalKey)

	put := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
		Body:   input.Body,
	}
	if input.ContentType != "" {
		put.ContentType = aws.String(input.ContentType)
	}
	if input.Size > 0 {
		put.ContentLength = aws.Int64(input.Size)
	}

	// No ACL is set: R2 has no per-object ACL model, and public delivery is
	// configured at the bucket / custom-domain level. The object is private to
	// the API's credentials and reachable to browsers only through
	// publicBaseURL.
	if _, err := s.client.PutObject(ctx, put); err != nil {
		return nil, fmt.Errorf("media: uploading %q to R2: %w", objectKey, sanitizeR2Error(err))
	}

	return &UploadedFile{
		Key:       input.Key,
		PublicURL: s.publicBaseURL + "/" + objectKey,
	}, nil
}

func (s *R2Storage) Delete(ctx context.Context, key string) error {
	logicalKey := strings.TrimSpace(key)
	if logicalKey == "" {
		return errors.New("media: delete requires a key")
	}
	objectKey := s.objectKey(logicalKey)

	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	}); err != nil {
		if isR2NotFound(err) {
			// "already gone" and "successfully removed" are the same outcome to
			// the caller — identical to LocalFilesystemStorage.Delete swallowing
			// os.IsNotExist. (S3/R2 DeleteObject is normally idempotent and
			// returns success for a missing key; this guards the rare case a
			// gateway surfaces a 404 instead.)
			return nil
		}
		return fmt.Errorf("media: deleting %q from R2: %w", objectKey, sanitizeR2Error(err))
	}
	return nil
}

// isR2NotFound reports whether err is R2/S3 signalling "no such key".
func isR2NotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return true
		}
	}
	return false
}

// sanitizeR2Error reduces an SDK error to its API code and message before it
// is wrapped and logged. R2Storage never holds the access key or secret (they
// live only in the S3 client's signer), and the SDK does not put credentials
// in errors, so this is defense in depth: it guarantees only the modelled
// error surface — never a raw request/response dump — can reach a log line.
func sanitizeR2Error(err error) error {
	if err == nil {
		return nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("%s: %s", apiErr.ErrorCode(), apiErr.ErrorMessage())
	}
	return err
}

// isNilR2API catches a typed-nil concrete client passed as the interface.
func isNilR2API(c r2API) bool {
	client, ok := c.(*s3.Client)
	return ok && client == nil
}

// compile-time guard: R2Storage must satisfy the interface the scheduling
// module depends on.
var _ MediaStorage = (*R2Storage)(nil)
