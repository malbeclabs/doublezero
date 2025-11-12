package timestamp

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/s3-uploader/internal/config"
)

// Generate creates a timestamped filename based on the given format.
func Generate(filePath string, format config.TimestampFormat) (string, error) {
	filename := filepath.Base(filePath)
	if filename == "." || filename == "/" {
		return "", fmt.Errorf("invalid filename: %s", filePath)
	}

	var timestamp string
	switch format {
	case config.TimestampFormatISO8601:
		// Format: 2025-11-05T12-30-45Z
		timestamp = time.Now().UTC().Format("2006-01-02T15-04-05Z")
	case config.TimestampFormatUnix:
		// Format: 1730815845
		timestamp = fmt.Sprintf("%d", time.Now().Unix())
	default:
		timestamp = time.Now().UTC().Format("2006-01-02T15-04-05Z")
	}

	timestampedName := fmt.Sprintf("%s_%s", timestamp, filename)
	return Sanitize(timestampedName), nil
}

// Sanitize removes or replaces characters that are not S3-safe.
// Allowed: alphanumeric, hyphen, underscore, period
func Sanitize(filename string) string {
	var result strings.Builder
	for _, c := range filename {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			result.WriteRune(c)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}
