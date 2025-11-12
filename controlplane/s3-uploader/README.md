# S3 Uploader

Standalone S3 uploader for ARISTA EOS with automatic timestamping, retry logic, and upload verification.

## Features

- Multiple configuration sources: TOML file, environment variables, CLI flags
- Automatic timestamping: ISO8601 or Unix timestamp formats
- Retry logic: Exponential backoff (1s, 2s, 4s, 8s, 16s) for 5 attempts
- Upload verification: POST-upload HEAD request with size validation
- Server-side encryption: AES256 encryption support
- Custom endpoints: Compatible with MinIO and S3-compatible services
- MD5 integrity: Content-MD5 header for upload integrity
- Graceful shutdown: Proper signal handling (SIGINT, SIGTERM)

## Installation

### Build from source

```bash
cd controlplane/s3-uploader
go build -o s3-uploader ./cmd/s3-uploader
```

### Build with version information

```bash
go build -ldflags="-X main.version=v1.0.0 -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o s3-uploader ./cmd/s3-uploader
```

## Usage

### Basic usage

```bash
s3-uploader /path/to/file.json
```

### With custom S3 key (no timestamping)

```bash
s3-uploader -key custom-name.json /path/to/file.json
```

### With custom bucket and region

```bash
s3-uploader -bucket my-bucket -region us-west-2 /path/to/file.json
```

### With config file

```bash
s3-uploader -config /path/to/config.toml /path/to/file.json
```

### Verbose logging

```bash
s3-uploader -verbose /path/to/file.json
```

### Version information

```bash
s3-uploader -version
```

## Configuration

Configuration is loaded with the following precedence (highest to lowest):

1. CLI flags (highest priority)
2. Environment variables
3. TOML config file
4. Defaults (lowest priority)

### Configuration File

Default path: `/mnt/flash/s3_uploader_config.toml`

Example configuration:

```toml
[aws]
region = "us-east-1"
bucket = "my-bucket"
access_key_id = "access-key-id"
secret_access_key = "secret-access-key"

# Optional: Custom S3 endpoint for MinIO
# endpoint_url = "http://localhost:9000"

[upload]
timestamp_format = "iso8601"  # or "unix"
enable_encryption = true
verify_upload = true
# key_prefix = "uploads"  # Optional prefix for all S3 keys
```

### Environment Variables

All configuration options can be set via environment variables with the `S3_UPLOADER_` prefix:

```bash
export S3_UPLOADER_AWS_REGION="us-east-1"
export S3_UPLOADER_AWS_BUCKET="my-bucket"
export S3_UPLOADER_AWS_ACCESS_KEY_ID="access-key-id"
export S3_UPLOADER_AWS_SECRET_ACCESS_KEY="secret-access-key"
export S3_UPLOADER_AWS_ENDPOINT_URL="http://localhost:9000"
export S3_UPLOADER_UPLOAD_TIMESTAMP_FORMAT="iso8601"
export S3_UPLOADER_UPLOAD_ENABLE_ENCRYPTION="true"
export S3_UPLOADER_UPLOAD_VERIFY_UPLOAD="true"
export S3_UPLOADER_UPLOAD_KEY_PREFIX="uploads"
```

### CLI Flags

```
  -bucket string
        AWS S3 bucket name (overrides config file)
  -config string
        Path to configuration file (default "/mnt/flash/s3_uploader_config.toml")
  -key string
        Custom S3 key (skips automatic timestamping)
  -region string
        AWS region (overrides config file)
  -verbose
        Enable verbose logging
  -version
        Print version information and exit
```

## Timestamp Formats

### ISO8601 (default)

Format: `2025-11-05T12-30-45Z_filename.json`

```toml
[upload]
timestamp_format = "iso8601"
```

### Unix

Format: `1730815845_filename.json`

```toml
[upload]
timestamp_format = "unix"
```

## Upload Behavior

1. File Reading: Reads entire file into memory
2. MD5 Calculation: Computes Content-MD5 for integrity
3. Timestamping: Adds timestamp prefix unless custom key specified
4. Retry Logic: Exponential backoff (1s → 2s → 4s → 8s → 16s)
5. Encryption: Optional AES256 server-side encryption
6. Verification: Optional POST-upload HEAD request with size check

## Filename Sanitization

Special characters in filenames are replaced with underscores for S3 compatibility:

- Allowed: `a-z`, `A-Z`, `0-9`, `-`, `_`, `.`
- All other characters → `_`

## Examples

### Upload to default bucket from config

```bash
s3-uploader /tmp/snapshot.json
# Output: https://my-bucket.s3.us-east-1.amazonaws.com/2025-11-05T12-30-45Z_snapshot.json
```

### Upload with custom key

```bash
s3-uploader -key production/snapshot-latest.json /tmp/snapshot.json
# Output: https://my-bucket.s3.us-east-1.amazonaws.com/production/snapshot-latest.json
```

### Upload to MinIO

```bash
export S3_UPLOADER_AWS_ENDPOINT_URL="http://minio:9000"
export S3_UPLOADER_AWS_BUCKET="backups"
s3-uploader /tmp/backup.tar.gz
# Output: http://minio:9000/backups/2025-11-05T12-30-45Z_backup.tar.gz
```

### Upload with prefix

```toml
[upload]
key_prefix = "device-1"
```

```bash
s3-uploader /tmp/metrics.json
# Output: https://my-bucket.s3.us-east-1.amazonaws.com/device-1/2025-11-05T12-30-45Z_metrics.json
```

## Testing

### Run unit tests

```bash
go test ./...
```

### Run tests with coverage

```bash
go test -cover ./...
```

### Run tests with verbose output

```bash
go test -v ./...
```

## Deployment on ARISTA EOS

1. Build the binary:

   ```bash
   GOOS=linux GOARCH=amd64 go build -o s3-uploader ./cmd/s3-uploader
   ```

2. Copy to device:

   ```bash
   scp s3-uploader admin@switch:/mnt/flash/
   ```

3. Create config file on device:

   ```bash
   ssh admin@switch
   cat > /mnt/flash/s3_uploader_config.toml << EOF
   [aws]
   region = "us-east-1"
   bucket = "device-uploads"
   access_key_id = "YOUR_KEY"
   secret_access_key = "YOUR_SECRET"

   [upload]
   timestamp_format = "iso8601"
   enable_encryption = true
   verify_upload = true
   EOF
   ```

4. Run uploader:
   ```bash
   /mnt/flash/s3-uploader /path/to/file.json
   ```

## Troubleshooting

### "Failed to load AWS config"

Check your AWS credentials in the config file or environment variables.

### "File not found"

Ensure the file path is correct and the file exists.

### "Upload verification failed: size mismatch"

The uploaded file size doesn't match the local file. This may indicate a network issue or S3 problem.

### "Failed after 5 attempts"

All retry attempts exhausted. Check your network connection and S3 service availability.
