#!/bin/bash
# Generic upload wrapper for ARISTA EOS
# This script can be customized for different data sources

# Configuration
TEMP_FILE="/tmp/upload_data.json"
S3_UPLOADER="/mnt/flash/s3-uploader"
CONFIG="/mnt/flash/s3_uploader_config.toml"

# Example: Capture ISIS database
# Uncomment and customize for your use case:
# FastCli -p 15 -c "show isis database detail | json" >"$TEMP_FILE"

# Example: Capture BGP information
# FastCli -p 15 -c "show ip bgp summary | json" >"$TEMP_FILE"

# Example: Upload existing file
# If you have a specific file to upload, you can skip the capture step
# TEMP_FILE="/path/to/your/file.json"

# Check if file was created/exists
if [ ! -f "$TEMP_FILE" ]; then
    echo "[ERROR] No data file to upload: $TEMP_FILE"
    exit 1
fi

# Upload via network namespace (if management VRF is used)
# If your switch has direct internet access, remove the 'sudo ip netns exec ns-management' part
if ip netns list | grep -q "ns-management"; then
    sudo ip netns exec ns-management "$S3_UPLOADER" -config "$CONFIG" "$TEMP_FILE"
else
    # Direct internet access (no management VRF)
    "$S3_UPLOADER" -config "$CONFIG" "$TEMP_FILE"
fi

UPLOAD_STATUS=$?

# Cleanup
rm -f "$TEMP_FILE"

exit $UPLOAD_STATUS
