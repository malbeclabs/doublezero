PREFIX:=github.com/malbeclabs/doublezero/tools/uping
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
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/uping-send cmd/uping-send/main.go
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/uping-recv cmd/uping-recv/main.go

FUZZTIME ?= 10s
.PHONY: fuzz
fuzz:
	@for f in $$(go test ./pkg/uping -list=Fuzz | grep '^Fuzz'); do \
		echo "==> Fuzzing $$f"; \
		go test ./pkg/uping -run=^$$ -fuzz=$$f -fuzztime=$(FUZZTIME) || exit 1; \
	done

