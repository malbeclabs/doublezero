TAG_PREFIX=ghcr.io/malbeclabs
TAG=doublezero-e2e
GIT_SHA:=`git rev-parse --short HEAD`

# Enable buildkit for docker mount caching.
# Enabled by default on mac, but not always on linux.
export DOCKER_BUILDKIT=1

.PHONY: eos
eos:
	docker build --platform linux/amd64 --build-arg CONFIG=e2e/fixtures/doublezero_device_startup_config_base.txt -f Dockerfile.eos -t agent:$(GIT_SHA) ../.

.PHONY: build
build: eos
	docker build --platform linux/amd64 -f Dockerfile -t $(TAG_PREFIX)/$(TAG):$(GIT_SHA) ../.

.PHONY: test
test: build
	bash bootstrap.sh

.PHONY: clean
clean:
	docker rmi $(TAG_PREFIX)/$(TAG):$(GIT_SHA)
	docker rmi agent:$(GIT_SHA)
