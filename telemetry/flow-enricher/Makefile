BUILD:=`git rev-parse --short HEAD` 
LDFLAGS=-ldflags "-X 'main.Build=$(BUILD)'"

.PHONY: test
test:
	go test -race -v ./...

.PHONY: lint
lint:
	golangci-lint run -c ../../.golangci.yaml

.PHONY: build
build:
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/enricher cmd/enricher/main.go
