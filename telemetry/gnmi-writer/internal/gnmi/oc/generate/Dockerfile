# Dockerfile for generating ygot Go structs from OpenConfig YANG models
#
# Usage:
#   docker build -t ygot-generator -f Dockerfile .
#   docker run --rm -v $(pwd)/..:/out ygot-generator
#
# This will generate oc.go in the parent directory (pkg/gnmi/oc/)

FROM golang:1.23-alpine AS builder

# Install git for cloning repos
RUN apk add --no-cache git

# Install ygot generator
RUN go install github.com/openconfig/ygot/generator@latest

# Clone OpenConfig public YANG models at v5.4.0
WORKDIR /yang
RUN git clone --depth 1 --branch v5.4.0 https://github.com/openconfig/public.git

# Clone third-party YANG models (IETF, etc.) that OpenConfig depends on
RUN git clone --depth 1 https://github.com/openconfig/yang.git /yang/third_party

# Create output directory
WORKDIR /out

# Generate Go structs from OpenConfig models
# We generate from network-instance (includes ISIS), interfaces, system,
# platform, and platform-transceiver (optical transceiver state)
# The -compress_paths flag enables OpenConfig path compression
# The -generate_fakeroot creates a root "Device" struct
#
# Key flags:
# - compress_paths: Simplifies paths per OpenConfig conventions
# - prefer_operational_state: Uses state/ paths as primary (for telemetry)
# - generate_fakeroot: Creates a root "Device" struct
# - generate_simple_unions: Uses simple union types (required for some models)
# - include_schema: Embeds schema for ytypes.Unmarshal operations
# - exclude_modules=ietf-interfaces: Avoids conflict with openconfig-interfaces
CMD ["sh", "-c", "\
    generator \
    -output_file=/out/oc.go \
    -package_name=oc \
    -generate_fakeroot \
    -fakeroot_name=Device \
    -compress_paths=true \
    -prefer_operational_state \
    -shorten_enum_leaf_names \
    -typedef_enum_with_defmod \
    -trim_enum_openconfig_prefix \
    -generate_simple_unions \
    -include_schema \
    -exclude_modules=ietf-interfaces \
    -path=/yang/public/release/models,/yang/public/release/models/types,/yang/public/third_party/ietf \
    /yang/public/release/models/network-instance/openconfig-network-instance.yang \
    /yang/public/release/models/interfaces/openconfig-interfaces.yang \
    /yang/public/release/models/system/openconfig-system.yang \
    /yang/public/release/models/platform/openconfig-platform.yang \
    /yang/public/release/models/platform/openconfig-platform-transceiver.yang \
    && echo 'Generated oc.go successfully' \
"]
