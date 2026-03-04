PREFIX:=github.com/malbeclabs/doublezero/client/doublezerod
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=-ldflags "-X=$(PREFIX)/build.Build=$(BUILD)"

.PHONY: test
test:
	go test -exec "sudo -E" -race -v ./...
	$(MAKE) test-e2e
	$(MAKE) test-faults

.PHONY: test-e2e
test-e2e:
	go run ../../tools/container-test/main.go -v

.PHONY: test-faults
test-faults:
	ulimit -n 65535
	go clean -testcache
	go test ./internal/liveness -v -run=TestClient_Liveness_Faults -clients=100 -stable-duration=10s

.PHONY: lint
lint:
	golangci-lint run -c ../../.golangci.yaml

.PHONY: build
build:
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/doublezerod cmd/doublezerod/main.go

FUZZTIME ?= 10s
.PHONY: fuzz
fuzz:
	@for f in $$(go test ./internal/liveness -list=Fuzz | grep '^Fuzz'); do \
		echo "==> Fuzzing $$f"; \
		go test ./internal/liveness -run=^$$ -fuzz=$$f -fuzztime=$(FUZZTIME) -timeout=60s || exit 1; \
	done
