PREFIX:=github.com/malbeclabs/doublezero/client/doublezerod
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=-ldflags "-X=$(PREFIX)/build.Build=$(BUILD)"

.PHONY: test
test:
	go test -race -v -coverprofile coverage.out ./...
	$(MAKE) test-e2e

.PHONY: test-e2e
test-e2e:
	go tool go-e2e -v

.PHONY: lint
lint:
	golangci-lint run -c ../../.golangci.yaml

.PHONY: build
build:
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/doublezerod cmd/doublezerod/main.go
