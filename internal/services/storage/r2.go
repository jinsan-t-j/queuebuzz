package storage

import (
	"bytes"
	"context"
	"fmt"
	"queuebuzz/internal/config"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type R2Service struct {
	cfg       *config.Config
	s3Client  *s3.Client
	bucket    string
	publicURL string
}

func NewR2Service(cfg *config.Config) (*R2Service, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.R2AccessKeyID, cfg.R2SecretAccessKey, "")),
		awsconfig.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.R2Endpoint)
		o.UsePathStyle = true
	})

	return &R2Service{
		cfg:       cfg,
		s3Client:  client,
		bucket:    cfg.R2BucketName,
		publicURL: strings.TrimSuffix(cfg.R2PublicURL, "/"),
	}, nil
}

// Upload uploads a file to R2 and returns the public URL.
// It sets Cache-Control to a long-lived value (1 year) as images are assumed to be versioned by the filename or replaced.
func (s *R2Service) Upload(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	_, err := s.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(data),
		ContentType:  aws.String(contentType),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to R2: %w", err)
	}

	return fmt.Sprintf("%s/%s", s.publicURL, key), nil
}

// Delete removes a file from R2.
func (s *R2Service) Delete(ctx context.Context, key string) error {
	_, err := s.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from R2: %w", err)
	}
	return nil
}

// GenerateKey creates a unique key for a file based on host ID and purpose.
func (s *R2Service) GenerateKey(hostID, purpose, ext string) string {
	// Format: hosts/<host_id>/<purpose>.<ext>
	// Using a fixed name per purpose ensures that re-uploads overwrite the same key,
	// but we'll use cache busting in the URL if needed, or rely on CDN invalidation.
	return fmt.Sprintf("hosts/%s/%s%s", hostID, purpose, ext)
}

// GetKeyFromURL extracts the R2 key from a public URL.
func (s *R2Service) GetKeyFromURL(url string) string {
	return strings.TrimPrefix(url, s.publicURL+"/")
}
