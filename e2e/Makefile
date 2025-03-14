TAG_PREFIX=ghcr.io/malbeclabs
TAG=doublezero-e2e
ARCH := $(shell uname -m)

.PHONY: solana-arm64
solana-arm64:
	docker build -f Dockerfile.solana.arm64 -t $(TAG_PREFIX)/solana-arm64:latest .

.PHONY: solana-amd64
solana-amd64:
	docker build --platform=linux/amd64 -f Dockerfile.solana.amd64 -t $(TAG_PREFIX)/solana-amd64:latest .

.PHONY: build
build:
ifeq ($(ARCH),arm64)
	docker build --build-arg PLATFORM_ARCH=arm64 -f Dockerfile -t $(TAG_PREFIX)/$(TAG):latest ../.
else
	docker build --build-arg PLATFORM_ARCH=amd64 -f Dockerfile -t $(TAG_PREFIX)/$(TAG):latest ../.
endif

.PHONY: all
all:
ifeq ($(ARCH),arm64)
	docker build -f Dockerfile.solana.arm64 -t $(TAG_PREFIX)/solana-arm64:latest .
	docker build --build-arg PLATFORM_ARCH=arm64 -f Dockerfile -t $(TAG_PREFIX)/$(TAG):latest ../.
else
	docker build --platform=linux/amd64 -f Dockerfile.solana.amd64 -t $(TAG_PREFIX)/solana-amd64:latest .
	docker build --build-arg PLATFORM_ARCH=amd64 -f Dockerfile -t $(TAG_PREFIX)/$(TAG):latest ../.
endif

.PHONY: test
test:
	docker run --privileged --rm $(TAG_PREFIX)/$(TAG):latest

.PHONY: pull
pull:
	docker pull $(TAG_PREFIX)/$(TAG):latest

.PHONY: push
push:
	docker push $(TAG_PREFIX)/$(TAG):latest
