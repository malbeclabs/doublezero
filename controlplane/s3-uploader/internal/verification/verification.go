package verification

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Verifier handles upload verification.
type Verifier struct {
	client *s3.Client
	bucket string
}

// New creates a new Verifier.
func New(client *s3.Client, bucket string) *Verifier {
	return &Verifier{
		client: client,
		bucket: bucket,
	}
}

// Verify checks that the uploaded file exists and has the expected size.
func (v *Verifier) Verify(ctx context.Context, key string, expectedSize int64) error {
	result, err := v.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &v.bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("failed to verify uploaded file: %w", err)
	}

	actualSize := *result.ContentLength
	if actualSize != expectedSize {
		return fmt.Errorf("size mismatch: expected %d bytes, got %d bytes", expectedSize, actualSize)
	}

	return nil
}
