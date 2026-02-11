PREFIX:=github.com/malbeclabs/doublezero/controlplane/telemetry
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=-ldflags "-X=$(PREFIX)/build.Build=$(BUILD)"

.PHONY: test
test:
	go test -race -v -coverprofile coverage.out ./...

.PHONY: lint
lint:
	golangci-lint run -c ../../.golangci.yaml

.PHONY: build
build:
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/doublezero-telemetry cmd/telemetry/main.go
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/doublezero-telemetry-data-cli cmd/data-cli/main.go
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/doublezero-telemetry-data-api cmd/data-api/main.go
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/doublezero-geoprobe-agent cmd/geoprobe-agent/main.go
