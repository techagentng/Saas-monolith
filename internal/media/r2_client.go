package media

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// R2ClientConfig is the non-secret-echoing input needed to build an S3 client
// pointed at a Cloudflare R2 bucket. Endpoint is the R2 S3 API base
// (https://<account>.r2.cloudflarestorage.com) with no bucket appended.
type R2ClientConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
}

// NewR2Client builds an AWS SDK for Go v2 S3 client configured for Cloudflare
// R2:
//
//   - region "auto" — R2 ignores the region but the SDK requires one to sign
//   - static credentials from configuration, never the ambient AWS
//     environment or shared credentials file
//   - the R2 S3 endpoint set as BaseEndpoint (current, non-deprecated SDK v2
//     API); the bucket is supplied per request and never concatenated onto it
//   - path-style addressing, which R2 handles more predictably than
//     virtual-host style for arbitrary bucket names
//
// It never logs, and never places in an error, the access key id or secret.
func NewR2Client(ctx context.Context, cfg R2ClientConfig) (*s3.Client, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("media: R2 client requires an endpoint")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("media: R2 client requires static credentials")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	)
	if err != nil {
		// LoadDefaultConfig only touches local/ambient config here (we override
		// region and credentials explicitly), so an error is a genuine
		// environment problem and carries no secret material.
		return nil, fmt.Errorf("media: loading R2 client configuration: %w", err)
	}

	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	}), nil
}
