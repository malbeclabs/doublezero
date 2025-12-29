package duck

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// LoadS3ConfigFromEnv loads S3 configuration from environment variables.
// Supports both AWS S3 and MinIO configurations, including IRSA (IAM Roles for Service Accounts).
//
// Environment variables:
//   - S3_ACCESS_KEY_ID or AWS_ACCESS_KEY_ID (optional, for IRSA leave unset to use IAM role)
//   - S3_SECRET_ACCESS_KEY or AWS_SECRET_ACCESS_KEY (optional, for IRSA leave unset to use IAM role)
//   - S3_ENDPOINT (optional, for MinIO: "http://localhost:9000")
//   - S3_REGION or AWS_REGION (optional, defaults to "us-east-1")
//   - S3_USE_SSL (optional, "true"/"false", auto-detected if S3_ENDPOINT is set)
//   - S3_URL_STYLE (optional, "path" or "virtual", auto-detected if S3_ENDPOINT is set)
//
// If S3_ENDPOINT is set, assumes MinIO and sets:
//   - UseSSL: false
//   - URLStyle: "path"
//
// Otherwise, assumes AWS S3 and sets:
//   - UseSSL: true
//   - URLStyle: "virtual"
//
// For IRSA (IAM Roles for Service Accounts) in AWS EKS, leave both access key and secret
// unset to use the IAM role credentials automatically.
func LoadS3ConfigFromEnv() (*S3Config, error) {
	// Get access key (try S3_ prefix first, then AWS_ prefix)
	accessKeyID := os.Getenv("S3_ACCESS_KEY_ID")
	if accessKeyID == "" {
		accessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}

	// Get secret key (try S3_ prefix first, then AWS_ prefix)
	secretAccessKey := os.Getenv("S3_SECRET_ACCESS_KEY")
	if secretAccessKey == "" {
		secretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}

	// If neither access key nor secret is set, return nil (not configured, will use IRSA/default credentials)
	if accessKeyID == "" && secretAccessKey == "" {
		return nil, nil // Not an error, just not configured - will use default AWS credentials chain (IRSA)
	}

	// If only secret is set without access key, that's an error
	if accessKeyID == "" && secretAccessKey != "" {
		return nil, fmt.Errorf("S3_SECRET_ACCESS_KEY or AWS_SECRET_ACCESS_KEY is set but S3_ACCESS_KEY_ID or AWS_ACCESS_KEY_ID is missing")
	}

	// If only access key is set without secret, that's also an error (inconsistent state)
	// For IRSA, both should be unset to use IAM role credentials
	if accessKeyID != "" && secretAccessKey == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY_ID or AWS_ACCESS_KEY_ID is set but S3_SECRET_ACCESS_KEY or AWS_SECRET_ACCESS_KEY is missing (for IRSA, leave both unset)")
	}

	// Get endpoint (optional, for MinIO)
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("AWS_ENDPOINT_URL")
	}

	// Get region (optional)
	region := os.Getenv("S3_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = "us-east-1" // Default region
	}

	// Determine if this is MinIO or AWS S3 based on endpoint
	isMinIO := endpoint != "" && !strings.Contains(endpoint, "amazonaws.com")

	// Set defaults based on whether it's MinIO or AWS
	var useSSL bool
	var urlStyle string

	urlStyle = "path"
	if isMinIO {
		// MinIO defaults
		useSSL = false
	} else {
		// AWS S3 defaults
		useSSL = true
	}

	// Override with explicit env vars if set
	if useSSLStr := os.Getenv("S3_USE_SSL"); useSSLStr != "" {
		useSSL = useSSLStr == "true" || useSSLStr == "1"
	}
	if urlStyleEnv := os.Getenv("S3_URL_STYLE"); urlStyleEnv != "" {
		urlStyle = urlStyleEnv
	}

	return &S3Config{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		Endpoint:        endpoint,
		Region:          region,
		UseSSL:          useSSL,
		URLStyle:        urlStyle,
	}, nil
}

// EnsureMinIOBucket checks if we're using localhost MinIO and creates the bucket if it doesn't exist.
func EnsureMinIOBucket(ctx context.Context, log *slog.Logger, storageURI string, s3Config *S3Config) error {
	// Only auto-create buckets for localhost MinIO
	if s3Config.Endpoint == "" {
		return nil // Not MinIO, skip
	}

	// Check if endpoint is localhost
	endpoint := s3Config.Endpoint
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	if !strings.HasPrefix(endpoint, "localhost") && !strings.HasPrefix(endpoint, "127.0.0.1") && !strings.Contains(endpoint, "host.docker.internal") {
		return nil // Not localhost, skip
	}

	// Extract bucket name from storage URI (format: s3://bucket-name/path)
	if !strings.HasPrefix(storageURI, "s3://") {
		return nil // Not an S3 URI
	}

	path := strings.TrimPrefix(storageURI, "s3://")
	parts := strings.SplitN(path, "/", 2)
	bucketName := parts[0]
	if bucketName == "" {
		return nil // No bucket name
	}

	// Create S3 client
	// MinIO always requires explicit credentials
	if s3Config.AccessKeyID == "" || s3Config.SecretAccessKey == "" {
		return fmt.Errorf("MinIO requires both S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY to be set")
	}
	creds := credentials.NewStaticCredentialsProvider(
		s3Config.AccessKeyID,
		s3Config.SecretAccessKey,
		"",
	)
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(s3Config.Region),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil {
		return fmt.Errorf("failed to create AWS config: %w", err)
	}

	// Ensure endpoint has protocol
	endpointURL := s3Config.Endpoint
	if !strings.HasPrefix(endpointURL, "http://") && !strings.HasPrefix(endpointURL, "https://") {
		endpointURL = "http://" + endpointURL
	}
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = &endpointURL
		o.UsePathStyle = true // Required for MinIO
	})

	// Check if bucket exists
	_, err = s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: &bucketName,
	})
	if err == nil {
		// Bucket exists, nothing to do
		return nil
	}

	// Bucket doesn't exist, create it
	log.Info("creating MinIO bucket", "bucket", bucketName, "endpoint", s3Config.Endpoint)
	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: &bucketName,
	})
	if err != nil {
		return fmt.Errorf("failed to create bucket %s: %w", bucketName, err)
	}

	log.Info("created MinIO bucket", "bucket", bucketName)
	return nil
}

// PrepareS3ConfigForStorageURI checks if storageURI is s3://, loads S3 config from environment,
// and ensures MinIO bucket exists if using localhost MinIO. Returns the S3Config if s3:// storage
// is used, or nil if file:// storage is used.
func PrepareS3ConfigForStorageURI(ctx context.Context, log *slog.Logger, storageURI string) (*S3Config, error) {
	if !strings.HasPrefix(storageURI, "s3://") {
		return nil, nil // Not S3 storage, no config needed
	}

	// Load S3 config from environment variables
	// If nil, that's OK - will use default AWS credentials chain (IRSA)
	s3Config, err := LoadS3ConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to load S3 configuration: %w", err)
	}
	// If s3Config is nil, create a minimal config with just region for IRSA
	if s3Config == nil {
		region := os.Getenv("S3_REGION")
		if region == "" {
			region = os.Getenv("AWS_REGION")
		}
		if region == "" {
			region = "us-east-1" // Default region
		}
		s3Config = &S3Config{
			AccessKeyID:     "", // Empty for IRSA
			SecretAccessKey: "", // Empty for IRSA
			Endpoint:        "", // AWS S3
			Region:          region,
			UseSSL:          true,   // AWS S3 default
			URLStyle:        "path", // AWS S3 default
		}
	}

	// Validate MinIO credentials: MinIO (any endpoint, not just localhost) requires explicit credentials
	// Check if this is MinIO (non-AWS endpoint)
	isMinIO := s3Config.Endpoint != "" && !strings.Contains(s3Config.Endpoint, "amazonaws.com")
	if isMinIO {
		if s3Config.AccessKeyID == "" || s3Config.SecretAccessKey == "" {
			return nil, fmt.Errorf("MinIO requires both S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY to be set (endpoint: %s)", s3Config.Endpoint)
		}
	}

	// If using localhost MinIO, ensure the bucket exists
	if err := EnsureMinIOBucket(ctx, log, storageURI, s3Config); err != nil {
		return nil, fmt.Errorf("failed to ensure MinIO bucket exists: %w", err)
	}

	return s3Config, nil
}
