package uploader

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/malbeclabs/doublezero/controlplane/s3-uploader/internal/config"
	"github.com/malbeclabs/doublezero/controlplane/s3-uploader/internal/retry"
	"github.com/malbeclabs/doublezero/controlplane/s3-uploader/internal/timestamp"
	"github.com/malbeclabs/doublezero/controlplane/s3-uploader/internal/verification"
)

// Uploader handles S3 uploads.
type Uploader struct {
	client   *s3.Client
	config   *config.Config
	verifier *verification.Verifier
	log      *slog.Logger
}

// New creates a new Uploader instance.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Uploader, error) {
	// Create AWS credentials
	creds := credentials.NewStaticCredentialsProvider(
		cfg.AWS.AccessKeyID,
		cfg.AWS.SecretAccessKey,
		"",
	)

	// Load AWS config
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.AWS.Region),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with optional custom endpoint
	var s3Client *s3.Client
	if cfg.AWS.EndpointURL != nil && *cfg.AWS.EndpointURL != "" {
		s3Client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = cfg.AWS.EndpointURL
			o.UsePathStyle = true // Required for MinIO and similar services
		})
		log.Info("[OK] Using custom S3 endpoint", "endpoint", *cfg.AWS.EndpointURL)
	} else {
		s3Client = s3.NewFromConfig(awsCfg)
	}

	verifier := verification.New(s3Client, cfg.AWS.Bucket)

	log.Info("[OK] S3 uploader initialized")

	return &Uploader{
		client:   s3Client,
		config:   cfg,
		verifier: verifier,
		log:      log,
	}, nil
}

// Upload uploads a file to S3 with optional timestamping and verification.
func (u *Uploader) Upload(ctx context.Context, filePath string, customKey *string) (string, error) {
	u.log.Info("[OK] Starting upload for file", "path", filePath)

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	dataSize := int64(len(data))
	contentMD5 := computeMD5(data)

	u.log.Info("[OK] File read", "bytes", dataSize, "md5", contentMD5)

	// Determine S3 key
	var s3Key string
	if customKey != nil && *customKey != "" {
		s3Key = *customKey
	} else {
		tsFilename, err := timestamp.Generate(filePath, u.config.GetTimestampFormat())
		if err != nil {
			return "", fmt.Errorf("failed to generate timestamped filename: %w", err)
		}

		if u.config.Upload.KeyPrefix != nil && *u.config.Upload.KeyPrefix != "" {
			s3Key = fmt.Sprintf("%s/%s", *u.config.Upload.KeyPrefix, tsFilename)
		} else {
			s3Key = tsFilename
		}
	}

	u.log.Info("[OK] S3 key", "key", s3Key)

	// Upload with retry
	if err := u.uploadWithRetry(ctx, s3Key, data, contentMD5); err != nil {
		return "", err
	}

	// Verify upload if enabled
	if u.config.Upload.VerifyUpload {
		u.log.Info("[OK] Verifying upload", "key", s3Key)
		if err := u.verifier.Verify(ctx, s3Key, dataSize); err != nil {
			return "", fmt.Errorf("upload verification failed: %w", err)
		}
		u.log.Info("[OK] Upload verified", "bytes", dataSize)
	}

	s3URL := u.getS3URL(s3Key)
	u.log.Info("[OK] Upload completed successfully", "url", s3URL)

	return s3URL, nil
}

// uploadWithRetry uploads to S3 with exponential backoff retry.
func (u *Uploader) uploadWithRetry(ctx context.Context, key string, data []byte, contentMD5 string) error {
	retryCfg := retry.DefaultConfig()

	uploadFunc := func() error {
		input := &s3.PutObjectInput{
			Bucket:     &u.config.AWS.Bucket,
			Key:        &key,
			Body:       newBytesReader(data),
			ContentMD5: &contentMD5,
		}

		if u.config.Upload.EnableEncryption {
			input.ServerSideEncryption = types.ServerSideEncryptionAes256
		}

		_, err := u.client.PutObject(ctx, input)
		if err != nil {
			u.log.Error("[ERROR] S3 upload failed", "error", err)
			return err
		}
		return nil
	}

	err := retry.Do(ctx, retryCfg, uploadFunc)
	if err != nil {
		return fmt.Errorf("S3 upload failed after retries: %w", err)
	}

	u.log.Info("[OK] Upload successful")
	return nil
}

// getS3URL constructs the S3 URL for the uploaded object.
func (u *Uploader) getS3URL(key string) string {
	if u.config.AWS.EndpointURL != nil && *u.config.AWS.EndpointURL != "" {
		return fmt.Sprintf("%s/%s/%s", *u.config.AWS.EndpointURL, u.config.AWS.Bucket, key)
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", u.config.AWS.Bucket, u.config.AWS.Region, key)
}

// computeMD5 computes the base64-encoded MD5 hash of the data.
func computeMD5(data []byte) string {
	hash := md5.Sum(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

// bytesReader implements io.ReadSeeker for []byte
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data, pos: 0}
}

func (b *bytesReader) Read(p []byte) (n int, err error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n = copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

func (b *bytesReader) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = int64(b.pos) + offset
	case io.SeekEnd:
		newPos = int64(len(b.data)) + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}

	if newPos < 0 {
		return 0, fmt.Errorf("negative position")
	}
	if newPos > int64(len(b.data)) {
		return 0, fmt.Errorf("position beyond end of data")
	}

	b.pos = int(newPos)
	return newPos, nil
}

func (b *bytesReader) Close() error {
	return nil
}
