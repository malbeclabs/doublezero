PREFIX:=github.com/malbeclabs/doublezero/tools/twamp
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
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/twamp-reflector cmd/twamp-reflector/main.go
	CGO_ENABLED=0 go build -v $(LDFLAGS) -o bin/twamp-sender cmd/twamp-sender/main.go

.PHONY: bench
bench: docker-build-reflector
	go test -bench=. -run=^$$ ./... -benchtime=50000x

.PHONY: fuzz
fuzz:
	go test -run=^$$ -fuzz=FuzzTWAMP_NTPTimestamp -fuzztime=10s -timeout=60s ./pkg/light

.PHONY: docker-build-reflector
docker-build-reflector:
	docker build -t dz-local/twamp-reflector:dev -f cmd/twamp-reflector/Dockerfile ../..
