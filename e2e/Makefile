TAG_PREFIX=ghcr.io/malbeclabs
TAG=doublezero-e2e
GIT_SHA:=`git rev-parse --short HEAD`

# Enable buildkit for docker mount caching.
# Enabled by default on mac, but not always on linux.
export DOCKER_BUILDKIT=1

.PHONY: build build-debug test test-debug test-nobuild test-keep test-keep-nobuild test-cleanup

# -----------------------------------------------------------------------------
# Build
# -----------------------------------------------------------------------------
build:
	go run ./cmd/dzctl/main.go build

build-debug:
	go run ./cmd/dzctl/main.go build -v

# -----------------------------------------------------------------------------
# Test
#
# This will build the docker images first, so it's not necessary to run `build`
# before running `test`.
#
# We configure -timeout=20m for the case where the user is running the tests
# sequentially. This should be more than enough time for the tests to run in
# that case, and leave room in case more tests are added in the future.
#
# Usage:
#   make test                           # run all tests
#   make test RUN=TestE2E_Multicast     # run a specific test
#   make test-debug RUN=TestE2E_Multicast # with debug logging
#   make test-nobuild                   # skip docker image build
#   make test-keep                      # keep containers after test
#   make test-keep-nobuild              # both
# -----------------------------------------------------------------------------
test:
	go test -tags=e2e -timeout=20m -v -count=1 $(if $(RUN),-run $(RUN)) .

test-debug:
	DEBUG=1 go test -tags=e2e -timeout=20m -v -count=1 $(if $(RUN),-run $(RUN)) .

test-nobuild:
	DZ_E2E_NO_BUILD=1 go test -tags=e2e -timeout=20m -v -count=1 $(if $(RUN),-run $(RUN)) .

test-keep:
	TESTCONTAINERS_RYUK_DISABLED=true go test -tags=e2e -timeout=20m -v -count=1 $(if $(RUN),-run $(RUN)) .

test-keep-nobuild:
	TESTCONTAINERS_RYUK_DISABLED=true DZ_E2E_NO_BUILD=1 go test -tags=e2e -timeout=20m -v -count=1 $(if $(RUN),-run $(RUN)) .

test-cleanup:
	@echo "Removing containers with label dz.malbeclabs.com..."
	@docker rm -f $$(docker ps -aq --filter label=dz.malbeclabs.com) 2>/dev/null || true
	@echo "Removing networks with label dz.malbeclabs.com..."
	@docker network rm $$(docker network ls -q --filter label=dz.malbeclabs.com) 2>/dev/null || true
	@echo "Removing volumes with label dz.malbeclabs.com..."
	@docker volume rm $$(docker volume ls -q --filter label=dz.malbeclabs.com) 2>/dev/null || true

# -----------------------------------------------------------------------------
# Solana image build and push.
#
# This is not required to run for the e2e tests, because it is expected that
# the solana image is already built and pushed to the container registry.
#
# If you need to rebuild the solana image, you can do that with the `buildx-solana`
# command, and then push it to the container registry with the
# `buildx-solana-push` command.
#
# The solana image is used by the e2e tests to run the solana validator.
# -----------------------------------------------------------------------------

.PHONY: .setup-multiplatform
.setup-multiplatform:
	docker buildx create --name multiplatform --driver docker-container --bootstrap --use 2>&1 | grep -v "existing instance" || true

.PHONY: buildx-solana
buildx-solana: .setup-multiplatform
	docker buildx build --platform linux/amd64,linux/arm64 -f docker/solana/Dockerfile -t $(TAG_PREFIX)/solana:latest .

.PHONY: buildx-solana-push
buildx-solana-push: .setup-multiplatform
	docker buildx build --platform linux/amd64,linux/arm64 -f docker/solana/Dockerfile -t $(TAG_PREFIX)/solana:latest --push .

.PHONY: proto
proto:
	docker run --rm -v $(PWD)/proto/qa:/workspace -w /workspace ghcr.io/jetexe/gobuf:1.61.0 generate
