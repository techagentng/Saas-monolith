package app

import (
	"context"
	"fmt"

	"github.com/techagentng/saas-monolith/internal/config"
	"github.com/techagentng/saas-monolith/internal/media"
)

// newMediaStorage builds the configured MediaStorage implementation. This is
// the one place a storage provider is chosen; everything above the
// media.MediaStorage interface (ServiceImageService and up) stays
// provider-agnostic.
//
// Both branches fail closed: config.Load has already required every field the
// chosen driver needs, and the R2 branch additionally surfaces a client- or
// constructor-level error rather than starting with a half-built storage.
func newMediaStorage(ctx context.Context, cfg config.Config) (media.MediaStorage, error) {
	switch cfg.MediaStorageDriver {
	case "local":
		return media.NewLocalFilesystemStorage(cfg.MediaLocalDir, cfg.MediaPublicBaseURL), nil
	case "r2":
		client, err := media.NewR2Client(ctx, media.R2ClientConfig{
			Endpoint:        cfg.MediaR2Endpoint,
			AccessKeyID:     cfg.MediaR2AccessKeyID,
			SecretAccessKey: cfg.MediaR2SecretAccessKey,
		})
		if err != nil {
			return nil, err
		}
		return media.NewR2Storage(client, cfg.MediaR2Bucket, cfg.MediaR2Prefix, cfg.MediaPublicBaseURL)
	default:
		return nil, fmt.Errorf("unsupported MEDIA_STORAGE_DRIVER %q", cfg.MediaStorageDriver)
	}
}
