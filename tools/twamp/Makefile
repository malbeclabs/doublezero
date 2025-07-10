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
bench:
	go test -bench=. -run=^$$ ./... -benchtime=10s

.PHONY: bench
bench-udp:
	go test -bench=. -run=^$$ ./pkg/udp -benchtime=10s

.PHONY: fuzz
fuzz:
	go test -fuzz=FuzzTWAMP_Sender -fuzztime=10s ./pkg/light
	go test -fuzz=FuzzTWAMP_Reflector -fuzztime=10s ./pkg/light
	go test -fuzz=FuzzTWAMP_NTPTimestamp -fuzztime=10s ./pkg/light
