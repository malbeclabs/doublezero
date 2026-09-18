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
# Budget the fuzz run by execution count, not wall time. A time budget makes the fuzz
# engine build a deadline context, and Go 1.25/1.26 have a race there (golang/go#75804,
# fixed in 1.27) that leaks "context deadline exceeded" as a spurious failure on loaded
# runners. A count budget never creates that context. 150000 execs is roughly the 10s
# this used to run for on CI; -timeout is the backstop now that wall time is unbounded.
	go test -run=^$$ -fuzz=FuzzTWAMP_NTPTimestamp -fuzztime=150000x -timeout=120s ./pkg/light

.PHONY: docker-build-reflector
docker-build-reflector:
	docker build -t dz-local/twamp-reflector:dev -f cmd/twamp-reflector/Dockerfile ../..
