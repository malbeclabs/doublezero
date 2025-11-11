package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Upload.TimestampFormat != "iso8601" {
		t.Errorf("expected timestamp_format to be 'iso8601', got %s", cfg.Upload.TimestampFormat)
	}
	if !cfg.Upload.EnableEncryption {
		t.Error("expected enable_encryption to be true")
	}
	if !cfg.Upload.VerifyUpload {
		t.Error("expected verify_upload to be true")
	}
}

func TestLoadFromTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `[aws]
region = "us-west-2"
bucket = "test-bucket"
access_key_id = "test-key"
secret_access_key = "test-secret"

[upload]
timestamp_format = "unix"
enable_encryption = false
verify_upload = false
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.AWS.Region != "us-west-2" {
		t.Errorf("expected region 'us-west-2', got %s", cfg.AWS.Region)
	}
	if cfg.AWS.Bucket != "test-bucket" {
		t.Errorf("expected bucket 'test-bucket', got %s", cfg.AWS.Bucket)
	}
	if cfg.Upload.TimestampFormat != "unix" {
		t.Errorf("expected timestamp_format 'unix', got %s", cfg.Upload.TimestampFormat)
	}
	if cfg.Upload.EnableEncryption {
		t.Error("expected enable_encryption to be false")
	}
	if cfg.Upload.VerifyUpload {
		t.Error("expected verify_upload to be false")
	}
}

func TestLoadWithEnvironmentVariables(t *testing.T) {
	// Set environment variables
	os.Setenv("S3_UPLOADER_AWS_REGION", "eu-west-1")
	os.Setenv("S3_UPLOADER_AWS_BUCKET", "env-bucket")
	os.Setenv("S3_UPLOADER_AWS_ACCESS_KEY_ID", "env-key")
	os.Setenv("S3_UPLOADER_AWS_SECRET_ACCESS_KEY", "env-secret")
	defer func() {
		os.Unsetenv("S3_UPLOADER_AWS_REGION")
		os.Unsetenv("S3_UPLOADER_AWS_BUCKET")
		os.Unsetenv("S3_UPLOADER_AWS_ACCESS_KEY_ID")
		os.Unsetenv("S3_UPLOADER_AWS_SECRET_ACCESS_KEY")
	}()

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.AWS.Region != "eu-west-1" {
		t.Errorf("expected region 'eu-west-1', got %s", cfg.AWS.Region)
	}
	if cfg.AWS.Bucket != "env-bucket" {
		t.Errorf("expected bucket 'env-bucket', got %s", cfg.AWS.Bucket)
	}
	if cfg.AWS.AccessKeyID != "env-key" {
		t.Errorf("expected access_key_id 'env-key', got %s", cfg.AWS.AccessKeyID)
	}
	if cfg.AWS.SecretAccessKey != "env-secret" {
		t.Errorf("expected secret_access_key 'env-secret', got %s", cfg.AWS.SecretAccessKey)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: &Config{
				AWS: AWSConfig{
					Region:          "us-east-1",
					Bucket:          "test-bucket",
					AccessKeyID:     "key",
					SecretAccessKey: "secret",
				},
				Upload: UploadConfig{
					TimestampFormat: "iso8601",
				},
			},
			wantErr: false,
		},
		{
			name: "empty region",
			cfg: &Config{
				AWS: AWSConfig{
					Region:          "",
					Bucket:          "test-bucket",
					AccessKeyID:     "key",
					SecretAccessKey: "secret",
				},
				Upload: UploadConfig{
					TimestampFormat: "iso8601",
				},
			},
			wantErr: true,
			errMsg:  "AWS region cannot be empty",
		},
		{
			name: "empty bucket",
			cfg: &Config{
				AWS: AWSConfig{
					Region:          "us-east-1",
					Bucket:          "",
					AccessKeyID:     "key",
					SecretAccessKey: "secret",
				},
				Upload: UploadConfig{
					TimestampFormat: "iso8601",
				},
			},
			wantErr: true,
			errMsg:  "AWS bucket cannot be empty",
		},
		{
			name: "invalid timestamp format",
			cfg: &Config{
				AWS: AWSConfig{
					Region:          "us-east-1",
					Bucket:          "test-bucket",
					AccessKeyID:     "key",
					SecretAccessKey: "secret",
				},
				Upload: UploadConfig{
					TimestampFormat: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid timestamp format: invalid. Must be 'iso8601' or 'unix'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if err.Error() != tt.errMsg {
					t.Errorf("Validate() error message = %v, want %v", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestApplyOverrides(t *testing.T) {
	cfg := &Config{
		AWS: AWSConfig{
			Region: "us-east-1",
			Bucket: "original-bucket",
		},
	}

	newBucket := "new-bucket"
	newRegion := "us-west-2"
	cfg.ApplyOverrides(&newBucket, &newRegion)

	if cfg.AWS.Bucket != "new-bucket" {
		t.Errorf("expected bucket 'new-bucket', got %s", cfg.AWS.Bucket)
	}
	if cfg.AWS.Region != "us-west-2" {
		t.Errorf("expected region 'us-west-2', got %s", cfg.AWS.Region)
	}
}

func TestGetTimestampFormat(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   TimestampFormat
	}{
		{
			name:   "iso8601",
			format: "iso8601",
			want:   TimestampFormatISO8601,
		},
		{
			name:   "unix",
			format: "unix",
			want:   TimestampFormatUnix,
		},
		{
			name:   "default to iso8601",
			format: "unknown",
			want:   TimestampFormatISO8601,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Upload: UploadConfig{
					TimestampFormat: tt.format,
				},
			}
			got := cfg.GetTimestampFormat()
			if got != tt.want {
				t.Errorf("GetTimestampFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}
