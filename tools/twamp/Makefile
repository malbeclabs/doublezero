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
# Budget by exec count, not wall time: a time budget builds a deadline context the
# Go 1.25/1.26 fuzz engine races on, leaking "context deadline exceeded" on loaded
# runners (golang/go#75804, fixed in 1.27). 150000 execs is about the 10s this ran
# for on CI; -timeout is the only bound left now that wall time is unbounded.
	go test -run=^$$ -fuzz=FuzzTWAMP_NTPTimestamp -fuzztime=150000x -timeout=120s ./pkg/light

.PHONY: docker-build-reflector
docker-build-reflector:
	docker build -t dz-local/twamp-reflector:dev -f cmd/twamp-reflector/Dockerfile ../..
