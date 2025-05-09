TAG_PREFIX=ghcr.io/malbeclabs
TAG=doublezero-e2e
ARCH := $(shell uname -m)

.PHONY: eos
eos:
	docker build --build-arg CONFIG=e2e/fixtures/doublezero_device_startup_config_base.txt -f Dockerfile.eos -t agent:latest ../.

.PHONY: solana-arm64
solana-arm64:
	docker build -f Dockerfile.solana.arm64 -t $(TAG_PREFIX)/solana-arm64:latest .

.PHONY: solana-amd64
solana-amd64:
	docker build --platform=linux/amd64 -f Dockerfile.solana.amd64 -t $(TAG_PREFIX)/solana-amd64:latest .

.PHONY: base
base:
ifeq ($(ARCH),arm64)
	docker build --build-arg PLATFORM_ARCH=arm64 --platform linux/arm64 -f Dockerfile.base -t $(TAG_PREFIX)/doublezero-base:latest ../.
else
	docker build --build-arg PLATFORM_ARCH=amd64 --platform linux/amd64 -f Dockerfile.base -t $(TAG_PREFIX)/doublezero-base:latest ../.
endif

.PHONY: build
build: eos
ifeq ($(ARCH),arm64)
	docker build --build-arg PLATFORM_ARCH=arm64 -f Dockerfile -t $(TAG_PREFIX)/$(TAG):latest ../.
else
	docker build --build-arg PLATFORM_ARCH=amd64 -f Dockerfile -t $(TAG_PREFIX)/$(TAG):latest ../.
endif

.PHONY: all
all: eos
ifeq ($(ARCH),arm64)
	docker build -f Dockerfile.solana.arm64 -t $(TAG_PREFIX)/solana-arm64:latest .
	docker build --build-arg PLATFORM_ARCH=arm64 -f Dockerfile -t $(TAG_PREFIX)/$(TAG):latest ../.
else
	docker build --platform=linux/amd64 -f Dockerfile.solana.amd64 -t $(TAG_PREFIX)/solana-amd64:latest .
	docker build --build-arg PLATFORM_ARCH=amd64 -f Dockerfile -t $(TAG_PREFIX)/$(TAG):latest ../.
endif

.PHONY: test
test:
	bash bootstrap.sh
