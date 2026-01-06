// Package isis provides S3 fetch capability for ISIS JSON data.
package isis

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	// DefaultBucket is the default S3 bucket for ISIS data.
	DefaultBucket = "doublezero-mn-beta-isis-db"
	// DefaultRegion is the default AWS region for the bucket.
	DefaultRegion = "us-east-1"
	// LatestPointerKey is the fallback pointer file if ListBucket fails.
	LatestPointerKey = "latest.json"
	// FileSuffix is the expected suffix for ISIS JSON files.
	FileSuffix = "_upload_data.json"
)

// S3Fetcher fetches ISIS JSON data from S3.
type S3Fetcher struct {
	client *s3.Client
	bucket string
}

// S3FetcherConfig holds configuration for creating an S3Fetcher.
type S3FetcherConfig struct {
	Bucket string
	Region string
}

// NewS3Fetcher creates a new S3Fetcher with anonymous credentials for public bucket access.
func NewS3Fetcher(cfg S3FetcherConfig) *S3Fetcher {
	bucket := cfg.Bucket
	if bucket == "" {
		bucket = DefaultBucket
	}

	region := cfg.Region
	if region == "" {
		region = DefaultRegion
	}

	// Use explicit anonymous credentials to avoid SDK credential chain issues
	// with public buckets. This prevents the SDK from trying IAM roles, env vars, etc.
	client := s3.New(s3.Options{
		Region:      region,
		Credentials: aws.AnonymousCredentials{},
	})

	return &S3Fetcher{
		client: client,
		bucket: bucket,
	}
}

// FetchLatestResult contains the result of fetching the latest ISIS JSON.
type FetchLatestResult struct {
	Key       string
	Timestamp string
	Body      io.ReadCloser
}

// FetchLatest retrieves the latest ISIS JSON file from S3.
// It uses StartAfter to minimize listing results by starting from yesterday's timestamp.
// If ListBucket fails, it falls back to fetching a latest.json pointer file.
func (f *S3Fetcher) FetchLatest(ctx context.Context) (*FetchLatestResult, error) {
	// Try listing first (optimized with StartAfter)
	key, err := f.findLatestKey(ctx)
	if err != nil {
		// Fallback: try to fetch latest.json pointer
		fallbackKey, fallbackErr := f.fetchLatestPointer(ctx)
		if fallbackErr != nil {
			return nil, fmt.Errorf("list bucket failed: %w; fallback to latest.json also failed: %v", err, fallbackErr)
		}
		key = fallbackKey
	}

	if key == "" {
		return nil, fmt.Errorf("no ISIS JSON files found in bucket %s", f.bucket)
	}

	// Fetch the actual file
	result, err := f.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(f.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", key, err)
	}

	timestamp := extractTimestampFromKey(key)

	return &FetchLatestResult{
		Key:       key,
		Timestamp: timestamp,
		Body:      result.Body,
	}, nil
}

// findLatestKey lists objects starting from yesterday to find the latest file.
func (f *S3Fetcher) findLatestKey(ctx context.Context) (string, error) {
	// Start from yesterday to minimize results while ensuring we catch recent files
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	startAfter := yesterday.Format("2006-01-02")

	input := &s3.ListObjectsV2Input{
		Bucket:     aws.String(f.bucket),
		StartAfter: aws.String(startAfter),
	}

	var keys []string

	paginator := s3.NewListObjectsV2Paginator(f.client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to list bucket: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.Key != nil && strings.HasSuffix(*obj.Key, FileSuffix) {
				keys = append(keys, *obj.Key)
			}
		}
	}

	if len(keys) == 0 {
		return "", nil
	}

	// Sort descending by key (timestamps in filename)
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	return keys[0], nil
}

// fetchLatestPointer attempts to fetch a latest.json pointer file.
// The pointer file contains just the key of the latest data file.
func (f *S3Fetcher) fetchLatestPointer(ctx context.Context) (string, error) {
	result, err := f.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(f.bucket),
		Key:    aws.String(LatestPointerKey),
	})
	if err != nil {
		return "", fmt.Errorf("failed to fetch %s: %w", LatestPointerKey, err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", LatestPointerKey, err)
	}

	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("empty %s pointer file", LatestPointerKey)
	}

	return key, nil
}

// extractTimestampFromKey extracts a human-readable timestamp from an S3 key.
// Example: 2026-01-06T15-42-13Z_upload_data.json -> "2026-01-06 15:42:13 UTC"
func extractTimestampFromKey(key string) string {
	// The key might have a path prefix, use just the filename part
	parts := strings.Split(key, "/")
	filename := parts[len(parts)-1]
	return extractTimestamp(filename)
}
