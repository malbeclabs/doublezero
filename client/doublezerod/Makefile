PREFIX:=github.com/malbeclabs/doublezero/client/doublezerod
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=-ldflags "-X=$(PREFIX)/build.Build=$(BUILD)"

.PHONY: test
test:
	go test -exec "sudo -E" -race -v ./...
	$(MAKE) test-e2e

.PHONY: test-e2e
test-e2e:
	go run ../../tools/e2e-test/main.go -v

.PHONY: lint
lint:
	golangci-lint run -c ../../.golangci.yaml

.PHONY: build
build:
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/doublezerod cmd/doublezerod/main.go
