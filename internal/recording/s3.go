package recording

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/dixydo/roomkit/internal/config"
)

// S3 wraps the aws-sdk-go-v2 S3 client tuned for DigitalOcean Spaces
// (path-style addressing, regional endpoint, ACL=public-read).
type S3 struct {
	cfg      config.RecordingConfig
	client   *s3.Client
	presign  *s3.PresignClient
	uploader *manager.Uploader
}

func NewS3(ctx context.Context, cfg config.RecordingConfig) (*S3, error) {
	if cfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3 bucket not configured")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3AccessKey, cfg.S3SecretKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}
		// DO Spaces tolerates virtual-hosted style as long as bucket name is
		// DNS-safe. UsePathStyle = false is the default and works.
	})

	up := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = 16 * 1024 * 1024 // 16 MiB parts
		u.Concurrency = 4
	})

	return &S3{cfg: cfg, client: client, presign: s3.NewPresignClient(client), uploader: up}, nil
}

// UploadFile streams localPath into s3://<bucket>/<key>. Sets the object's
// ACL only when ROOMKIT_S3_OBJECT_ACL is non-empty — DigitalOcean Spaces
// rejects ACL headers when the Space has "Block ACL modifications" enabled
// (the new default), so the safe path is to set per-bucket public-read in
// the DO console and skip per-object ACL here.
func (s *S3) UploadFile(ctx context.Context, localPath, key, contentType string) (string, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.S3Bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String(contentType),
	}
	if s.cfg.S3ObjectACL != "" {
		input.ACL = s3types.ObjectCannedACL(s.cfg.S3ObjectACL)
	}

	if _, err := s.uploader.Upload(ctx, input); err != nil {
		return "", fmt.Errorf("s3 upload: %w", err)
	}
	return s.publicURL(key), nil
}

// UploadBytes uploads an in-memory payload — used by the --s3-test diagnostic.
func (s *S3) UploadBytes(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.S3Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	}
	if s.cfg.S3ObjectACL != "" {
		input.ACL = s3types.ObjectCannedACL(s.cfg.S3ObjectACL)
	}
	if _, err := s.uploader.Upload(ctx, input); err != nil {
		return "", err
	}
	return s.publicURL(key), nil
}

// PresignedURL returns a time-limited GET URL for a private object.
func (s *S3) PresignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.S3Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign: %w", err)
	}
	return req.URL, nil
}

func (s *S3) publicURL(key string) string {
	if s.cfg.S3PublicBase != "" {
		return strings.TrimRight(s.cfg.S3PublicBase, "/") + "/" + key
	}
	// Derive from endpoint + bucket.
	if s.cfg.S3Endpoint != "" {
		u, err := url.Parse(s.cfg.S3Endpoint)
		if err == nil {
			// Virtual-hosted style: https://<bucket>.<host>/<key>
			return fmt.Sprintf("%s://%s.%s/%s", u.Scheme, s.cfg.S3Bucket, u.Host, key)
		}
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.cfg.S3Bucket, s.cfg.S3Region, key)
}
