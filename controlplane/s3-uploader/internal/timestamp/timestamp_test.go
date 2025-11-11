package timestamp

import (
	"strings"
	"testing"

	"github.com/malbeclabs/doublezero/controlplane/s3-uploader/internal/config"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal filename",
			input:    "normal-file_123.txt",
			expected: "normal-file_123.txt",
		},
		{
			name:     "special characters",
			input:    "test file!@#.json",
			expected: "test_file___.json",
		},
		{
			name:     "spaces",
			input:    "file with spaces.txt",
			expected: "file_with_spaces.txt",
		},
		{
			name:     "unicode characters",
			input:    "文件名.txt",
			expected: "___.txt",
		},
		{
			name:     "only alphanumeric",
			input:    "abc123XYZ",
			expected: "abc123XYZ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sanitize(tt.input)
			if got != tt.expected {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		format   config.TimestampFormat
		wantErr  bool
	}{
		{
			name:     "iso8601 format",
			filePath: "/tmp/test.json",
			format:   config.TimestampFormatISO8601,
			wantErr:  false,
		},
		{
			name:     "unix format",
			filePath: "/tmp/test.json",
			format:   config.TimestampFormatUnix,
			wantErr:  false,
		},
		{
			name:     "relative path",
			filePath: "test.txt",
			format:   config.TimestampFormatISO8601,
			wantErr:  false,
		},
		{
			name:     "with special characters",
			filePath: "/tmp/test file!.json",
			format:   config.TimestampFormatISO8601,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Generate(tt.filePath, tt.format)
			if (err != nil) != tt.wantErr {
				t.Errorf("Generate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Check that the result contains the original filename
				if !strings.Contains(got, "test") {
					t.Errorf("Generate() result %q doesn't contain original filename", got)
				}

				// Check format-specific requirements
				switch tt.format {
				case config.TimestampFormatISO8601:
					// Should contain date-like pattern and T
					if !strings.Contains(got, "T") || !strings.Contains(got, "Z") {
						t.Errorf("Generate() with ISO8601 format = %q, expected ISO8601 format with T and Z", got)
					}
				case config.TimestampFormatUnix:
					// Should start with digits
					if len(got) < 10 || got[0] < '0' || got[0] > '9' {
						t.Errorf("Generate() with Unix format = %q, expected to start with timestamp digits", got)
					}
				}

				// Check that special characters are sanitized
				if strings.ContainsAny(got, "!@#$ ") {
					t.Errorf("Generate() result %q contains unsanitized special characters", got)
				}
			}
		})
	}
}

func TestGenerateConsistentFormat(t *testing.T) {
	// Test that the same format produces consistent structure
	result1, err1 := Generate("/tmp/test.json", config.TimestampFormatISO8601)
	result2, err2 := Generate("/tmp/test.json", config.TimestampFormatISO8601)

	if err1 != nil || err2 != nil {
		t.Fatalf("Generate() failed: err1=%v, err2=%v", err1, err2)
	}

	// Both should have similar structure (timestamp_filename.json)
	parts1 := strings.Split(result1, "_")
	parts2 := strings.Split(result2, "_")

	if len(parts1) < 2 || len(parts2) < 2 {
		t.Errorf("Expected results to have timestamp_filename structure, got %q and %q", result1, result2)
	}
}
