# ARISTA EOS Deployment Guide

How to deploy and schedule automated file uploads to S3 on ARISTA EOS switches.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Creating Upload Scripts](#creating-upload-scripts)
- [Scheduling](#scheduling)
- [Verification](#verification)
- [Troubleshooting](#troubleshooting)
- [Examples](#examples)

## Prerequisites

Before proceeding, ensure you have:

1. **AWS Credentials**
   - AWS Access Key ID
   - AWS Secret Access Key
   - S3 bucket name
   - AWS region (e.g., `us-east-1`)

2. **Switch Access**
   - Admin/sudo access to ARISTA EOS switch
   - SSH or console access
   - Write access to `/mnt/flash/`

3. **Network Connectivity**
   - Internet access (may require management VRF/namespace)
   - Outbound HTTPS access to AWS S3 endpoints

## Installation

### Option 1: Install from RPM (Recommended)

The RPM package includes the binary, example config, and wrapper script.

#### Download RPM

```bash
# On your workstation
wget https://github.com/malbeclabs/doublezero/releases/download/s3-uploader/v1.0.0/doublezero-s3-uploader-1.0.0-1.x86_64.rpm
```

#### Copy to Switch

```bash
# Copy RPM to switch
scp doublezero-s3-uploader-1.0.0-1.x86_64.rpm admin@switch-hostname:/mnt/flash/
```

#### Install RPM

```bash
# SSH to switch
ssh admin@switch-hostname

# Enter bash shell
bash

# Install the RPM (requires sudo)
sudo rpm -ivh /mnt/flash/doublezero-s3-uploader-1.0.0-1.x86_64.rpm

# Verify installation
ls -lh /mnt/flash/s3-uploader
ls -lh /mnt/flash/s3_uploader_config.toml.example
ls -lh /mnt/flash/upload-wrapper.sh
```

Expected output:

```
-rwxr-xr-x+ 1 root root  13M Nov 11 22:00 /mnt/flash/s3-uploader
-rw-r--r--+ 1 root root  500 Nov 11 22:00 /mnt/flash/s3_uploader_config.toml.example
-rwxr-xr-x+ 1 root root  1.2K Nov 11 22:00 /mnt/flash/upload-wrapper.sh
```

### Option 2: Manual Installation

If you can't use RPM, install manually:

```bash
# Build on your workstation
cd controlplane/s3-uploader
make build-linux

# Copy binary to switch
scp bin/s3-uploader-linux-amd64 admin@switch-hostname:/mnt/flash/s3-uploader

# Copy example config
scp example-config.toml admin@switch-hostname:/mnt/flash/s3_uploader_config.toml.example

# Copy wrapper script
scp scripts/upload-wrapper.sh admin@switch-hostname:/mnt/flash/

# SSH to switch and make files executable
ssh admin@switch-hostname
bash
sudo chmod +x /mnt/flash/s3-uploader
sudo chmod +x /mnt/flash/upload-wrapper.sh
```

## Configuration

### Step 1: Create Configuration File

Create `/mnt/flash/s3_uploader_config.toml` with your AWS credentials:

```bash
cat > /mnt/flash/s3_uploader_config.toml << 'EOF'
[aws]
# AWS region (required)
region = "us-east-1"

# S3 bucket name (required)
bucket = "your-bucket-name"

# AWS access key ID (required)
access_key_id = "your-access-key-id"

# AWS secret access key (required)
secret_access_key = "your-secret-access-key"

# Optional: Custom S3 endpoint for MinIO or S3-compatible storage
# endpoint_url = "http://localhost:9000"

[upload]
# Timestamp format: "iso8601" or "unix"
# iso8601: 2025-11-11T12-30-45Z_filename.json
# unix: 1731358000_filename.json
timestamp_format = "iso8601"

# Enable server-side encryption (AES256)
enable_encryption = true

# Verify upload after completion (HEAD request + size check)
verify_upload = true

# Optional: Prefix for S3 keys
# key_prefix = "device-uploads"
EOF
```

**IMPORTANT**: Replace `your-bucket-name`, `your-access-key-id`, and `your-secret-access-key` with your actual AWS credentials.

### Step 2: Secure the Configuration File

```bash
# Restrict permissions (credentials are sensitive!)
sudo chmod 600 /mnt/flash/s3_uploader_config.toml
```

This ensures only root can read the file containing AWS credentials.

### Step 3: Test Configuration

```bash
# Create a test file
echo '{"test": "data"}' > /tmp/test.json

# Test upload (with management namespace)
sudo ip netns exec ns-management /mnt/flash/s3-uploader -config /mnt/flash/s3_uploader_config.toml /tmp/test.json

# OR test without management namespace
/mnt/flash/s3-uploader -config /mnt/flash/s3_uploader_config.toml /tmp/test.json

# Clean up
rm /tmp/test.json
```

Expected output:

```
time=2025-11-11T22:00:00.000+04:00 level=INFO msg="[OK] S3 Uploader starting..."
time=2025-11-11T22:00:00.001+04:00 level=INFO msg="[OK] Loading configuration"
time=2025-11-11T22:00:00.001+04:00 level=INFO msg="[OK] Configuration validated"
...
[OK] Upload successful!
[OK] S3 URL: https://your-bucket.s3.us-east-1.amazonaws.com/2025-11-11T18-00-00Z_test.json
```

## Creating Upload Scripts

Create `/mnt/flash/upload-isis-snapshot.sh`:

```bash
cat > /mnt/flash/upload-isis-snapshot.sh << 'EOF'
#!/bin/bash
# Capture ISIS database and upload to S3

TEMP_FILE="/tmp/isis_database.json"
S3_UPLOADER="/mnt/flash/s3-uploader"
CONFIG="/mnt/flash/s3_uploader_config.toml"

# Capture ISIS database to JSON
FastCli -p 15 -c "show isis database detail | json" >"$TEMP_FILE"

# Upload via network namespace (if management VRF is used)
if ip netns list | grep -q "ns-management"; then
    sudo ip netns exec ns-management "$S3_UPLOADER" -config "$CONFIG" "$TEMP_FILE"
else
    "$S3_UPLOADER" -config "$CONFIG" "$TEMP_FILE"
fi

# Cleanup
rm -f "$TEMP_FILE"
EOF

chmod +x /mnt/flash/upload-isis-snapshot.sh
```

## Scheduling

### Step 1: Enter Configuration Mode

```bash
# From EOS CLI
configure terminal
```

### Step 2: Create Schedule

#### Production Schedule (Every 6 Hours)

```bash
schedule isis-upload now interval 360 timeout 2 max-log-files 100 command bash /mnt/flash/upload-isis-snapshot.sh
```

#### More Frequent (Hourly)

```bash
schedule bgp-upload now interval 60 timeout 2 max-log-files 100 command bash /mnt/flash/upload-bgp-summary.sh
```

#### Daily Schedule (Midnight)

```bash
schedule daily-snapshot at 00:00:00 interval 1440 timeout 5 max-log-files 30 command bash /mnt/flash/upload-file.sh
```

**Schedule Parameters:**

- `now` or `at HH:MM:SS` - Start time
- `interval <minutes>` - How often to run
- `timeout <minutes>` - Maximum execution time
- `max-log-files <number>` - Log files to keep
- `command bash <script>` - Command to execute

### Step 3: Save Configuration

```bash
write memory
```

## Verification

### Check Schedule Status

```bash
show schedule summary
```

Expected output:

```
Maximum concurrent jobs  1
Prepend host name to logfile: Yes
Name                 At Time       Last        Interval       Timeout        Max        Max     Logfile Location                  Status
                                   Time         (mins)        (mins)         Log        Logs
                                                                            Files       Size
----------------- ------------- ----------- -------------- ------------- ----------- ---------- --------------------------------- ------
isis-upload            now         10:39          360            2           100         -      flash:schedule/isis-upload/       Success
```

**Status Meanings:**

- `Success` - Last execution completed successfully
- `Fail` - Last execution failed (check logs)
- `Timeout` - Execution exceeded timeout limit

### Check Logs

```bash
# Enter bash shell
bash

# Navigate to schedule logs
cd /mnt/flash/schedule/isis-upload/

# List log files
ls -lh

# View latest log
ls -t | head -1 | xargs zcat  # If compressed
# OR
ls -t | head -1 | xargs cat   # If not compressed
```

### Verify S3 Uploads

Using AWS CLI:

```bash
aws s3 ls s3://your-bucket-name/ --human
```

Or check the S3 console at https://console.aws.amazon.com/s3/

## Troubleshooting

### Schedule Shows "Fail" Status

**Check the logs:**

```bash
bash
cd /mnt/flash/schedule/<schedule-name>/
ls -lt | head -5  # View most recent logs
zcat *.gz | tail -50  # View compressed logs
```

### Common Errors and Solutions

| **Error**                                         | **Cause**                 | **Solution**                                           |
| ------------------------------------------------- | ------------------------- | ------------------------------------------------------ |
| `Permission denied`                               | Script not executable     | `chmod +x /mnt/flash/<script>.sh`                      |
| `No such file or directory`                       | Binary or script missing  | Verify RPM installation                                |
| `command not found`                               | Incorrect schedule syntax | Use `command bash /mnt/flash/<script>.sh`              |
| `Network unreachable`                             | No internet access        | Check management namespace routing                     |
| `Access Denied (S3)`                              | Invalid AWS credentials   | Verify credentials in config file                      |
| `NoSuchBucket`                                    | Bucket doesn't exist      | Create S3 bucket or fix bucket name                    |
| `Configuration error: AWS region cannot be empty` | Missing config            | Check `/mnt/flash/s3_uploader_config.toml` exists      |
| `Upload verification failed`                      | Size mismatch             | Check network connectivity, try disabling verification |

### Testing Network Connectivity

```bash
# Test from management namespace
sudo ip netns exec ns-management ping -c 3 s3.amazonaws.com

# Test S3 access
sudo ip netns exec ns-management curl -I https://your-bucket.s3.us-east-1.amazonaws.com/

# Test DNS resolution
sudo ip netns exec ns-management nslookup s3.amazonaws.com
```

### Check Uploader Version

```bash
/mnt/flash/s3-uploader -version
```

### Manual Test Upload

```bash
# Create test file
echo '{"test": "manual"}' > /tmp/manual-test.json

# Upload with verbose logging
sudo ip netns exec ns-management /mnt/flash/s3-uploader \
  -config /mnt/flash/s3_uploader_config.toml \
  -verbose \
  /tmp/manual-test.json

# Clean up
rm /tmp/manual-test.json
```

## Examples

### Complete ISIS Upload Setup

```bash
# 1. Install (assumes RPM is already copied to switch)
bash
sudo rpm -ivh /mnt/flash/doublezero-s3-uploader-1.0.0-1.x86_64.rpm

# 2. Configure
cat > /mnt/flash/s3_uploader_config.toml << 'EOF'
[aws]
region = "us-east-1"
bucket = "network-telemetry"
access_key_id = "YOUR_KEY"
secret_access_key = "YOUR_SECRET"

[upload]
timestamp_format = "iso8601"
enable_encryption = true
verify_upload = true
key_prefix = "isis-databases"
EOF

sudo chmod 600 /mnt/flash/s3_uploader_config.toml

# 3. Create upload script
cat > /mnt/flash/upload-isis.sh << 'EOF'
#!/bin/bash
TEMP_FILE="/tmp/isis.json"
FastCli -p 15 -c "show isis database detail | json" >"$TEMP_FILE"
sudo ip netns exec ns-management /mnt/flash/s3-uploader -config /mnt/flash/s3_uploader_config.toml "$TEMP_FILE"
rm -f "$TEMP_FILE"
EOF

chmod +x /mnt/flash/upload-isis.sh

# 4. Test
bash /mnt/flash/upload-isis.sh

# 5. Schedule (from EOS CLI)
# conf t
# schedule isis-upload now interval 360 timeout 2 max-log-files 100 command bash /mnt/flash/upload-isis.sh
# write memory
```
