TAG_PREFIX=ghcr.io/malbeclabs
TAG=doublezero-e2e
GIT_SHA:=`git rev-parse --short HEAD`
ARCH:=$(shell if [ "$(shell uname -m)" = "aarch64" ]; then echo "arm64"; elif [ "$(shell uname -m)" = "x86_64" ]; then echo "amd64"; else echo "$(shell uname -m)"; fi)

# Enable buildkit for docker mount caching.
# Enabled by default on mac, but not always on linux.
export DOCKER_BUILDKIT=1

.PHONY: eos
eos:
	docker build --platform linux/${ARCH} --build-arg CONFIG=e2e/fixtures/doublezero_device_startup_config_base.txt -f Dockerfile.eos -t agent:$(GIT_SHA) ../.

.PHONY: solana
solana:
	docker build --platform linux/${ARCH} -f Dockerfile.solana.${ARCH} -t $(TAG_PREFIX)/solana-${ARCH}:latest ../.

.PHONY: build
build: eos solana
	docker build --platform linux/${ARCH} --build-arg PLATFORM_ARCH=${ARCH} -f Dockerfile -t $(TAG_PREFIX)/$(TAG):$(GIT_SHA) ../.

.PHONY: test
test: build
	bash bootstrap.sh

.PHONY: clean
clean:
	docker rmi $(TAG_PREFIX)/$(TAG):$(GIT_SHA)
	docker rmi agent:$(GIT_SHA)
