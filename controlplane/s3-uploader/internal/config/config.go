package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config represents the complete configuration for the S3 uploader.
type Config struct {
	AWS    AWSConfig    `toml:"aws"`
	Upload UploadConfig `toml:"upload"`
}

// AWSConfig contains AWS-specific configuration.
type AWSConfig struct {
	Region          string  `toml:"region"`
	Bucket          string  `toml:"bucket"`
	AccessKeyID     string  `toml:"access_key_id"`
	SecretAccessKey string  `toml:"secret_access_key"`
	EndpointURL     *string `toml:"endpoint_url,omitempty"`
}

// UploadConfig contains upload-specific configuration.
type UploadConfig struct {
	TimestampFormat  string  `toml:"timestamp_format"`
	EnableEncryption bool    `toml:"enable_encryption"`
	VerifyUpload     bool    `toml:"verify_upload"`
	KeyPrefix        *string `toml:"key_prefix,omitempty"`
}

// TimestampFormat represents the supported timestamp formats.
type TimestampFormat int

const (
	TimestampFormatISO8601 TimestampFormat = iota
	TimestampFormatUnix
)

// DefaultConfig returns a Config with default values.
func DefaultConfig() *Config {
	return &Config{
		Upload: UploadConfig{
			TimestampFormat:  "iso8601",
			EnableEncryption: true,
			VerifyUpload:     true,
			KeyPrefix:        nil,
		},
	}
}

// Load loads configuration from a TOML file, environment variables, and applies defaults.
// Priority: CLI flags > Environment variables > Config file > Defaults
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	// Load from TOML file if it exists
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse TOML config: %w", err)
		}
	}

	// Override with environment variables
	if v := os.Getenv("S3_UPLOADER_AWS_REGION"); v != "" {
		cfg.AWS.Region = v
	}
	if v := os.Getenv("S3_UPLOADER_AWS_BUCKET"); v != "" {
		cfg.AWS.Bucket = v
	}
	if v := os.Getenv("S3_UPLOADER_AWS_ACCESS_KEY_ID"); v != "" {
		cfg.AWS.AccessKeyID = v
	}
	if v := os.Getenv("S3_UPLOADER_AWS_SECRET_ACCESS_KEY"); v != "" {
		cfg.AWS.SecretAccessKey = v
	}
	if v := os.Getenv("S3_UPLOADER_AWS_ENDPOINT_URL"); v != "" {
		cfg.AWS.EndpointURL = &v
	}
	if v := os.Getenv("S3_UPLOADER_UPLOAD_TIMESTAMP_FORMAT"); v != "" {
		cfg.Upload.TimestampFormat = v
	}
	if v := os.Getenv("S3_UPLOADER_UPLOAD_ENABLE_ENCRYPTION"); v != "" {
		cfg.Upload.EnableEncryption = v == "true" || v == "1"
	}
	if v := os.Getenv("S3_UPLOADER_UPLOAD_VERIFY_UPLOAD"); v != "" {
		cfg.Upload.VerifyUpload = v == "true" || v == "1"
	}
	if v := os.Getenv("S3_UPLOADER_UPLOAD_KEY_PREFIX"); v != "" {
		cfg.Upload.KeyPrefix = &v
	}

	return cfg, nil
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.AWS.Region == "" {
		return fmt.Errorf("AWS region cannot be empty")
	}
	if c.AWS.Bucket == "" {
		return fmt.Errorf("AWS bucket cannot be empty")
	}
	if c.AWS.AccessKeyID == "" {
		return fmt.Errorf("AWS access_key_id cannot be empty")
	}
	if c.AWS.SecretAccessKey == "" {
		return fmt.Errorf("AWS secret_access_key cannot be empty")
	}

	// Validate timestamp format
	if c.Upload.TimestampFormat != "iso8601" && c.Upload.TimestampFormat != "unix" {
		return fmt.Errorf("invalid timestamp format: %s. Must be 'iso8601' or 'unix'", c.Upload.TimestampFormat)
	}

	return nil
}

// ApplyOverrides applies CLI flag overrides to the configuration.
func (c *Config) ApplyOverrides(bucket, region *string) {
	if bucket != nil && *bucket != "" {
		c.AWS.Bucket = *bucket
	}
	if region != nil && *region != "" {
		c.AWS.Region = *region
	}
}

// GetTimestampFormat returns the parsed TimestampFormat enum.
func (c *Config) GetTimestampFormat() TimestampFormat {
	if c.Upload.TimestampFormat == "unix" {
		return TimestampFormatUnix
	}
	return TimestampFormatISO8601
}
