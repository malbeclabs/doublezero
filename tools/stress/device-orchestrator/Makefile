PREFIX:=github.com/malbeclabs/doublezero/tools/stress/device-orchestrator
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=-ldflags "-X=$(PREFIX)/build.Build=$(BUILD)"

.PHONY: test
test:
	go test -race -v -coverprofile coverage.out ./...

.PHONY: lint
lint:
	golangci-lint run -c ../../../.golangci.yaml

.PHONY: build
build:
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/device-orchestrator cmd/device-orchestrator/main.go
