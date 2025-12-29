.PHONY: build run dev docker-build docker-up docker-down clean test

BUILD := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X 'main.Build=$(BUILD)'"

# Build the application
build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/flow-analytics ./cmd/flow-analytics

# Run locally (requires ClickHouse running)
run: build
	./bin/flow-analytics

# Development mode with auto-reload (requires air: go install github.com/cosmtrek/air@latest)
dev:
	air

# Build Docker image
docker-build:
	docker build -t flow-analytics:latest .

# Start full stack with Docker Compose
docker-up:
	docker-compose up -d

# Stop Docker Compose stack
docker-down:
	docker-compose down

# View logs
logs:
	docker-compose logs -f flow-analytics

# Clean build artifacts
clean:
	rm -rf bin/
	docker-compose down -v

# Run tests
test:
	go test -v ./...

# Lint code
lint:
	golangci-lint run

# Download dependencies
deps:
	go mod download
	go mod tidy

# Connect to ClickHouse CLI
clickhouse-cli:
	docker-compose exec clickhouse clickhouse-client

# Reset database (drop and recreate)
db-reset:
	docker-compose exec clickhouse clickhouse-client --query "DROP TABLE IF EXISTS default.flows_testnet"
	docker-compose exec clickhouse clickhouse-client --multiquery < init-db/01_create_tables.sql

# Generate more sample data
db-seed:
	docker-compose exec clickhouse clickhouse-client --multiquery < init-db/01_create_tables.sql

# Show table statistics
db-stats:
	docker-compose exec clickhouse clickhouse-client --query "SELECT count() as rows, formatReadableSize(sum(bytes)) as size FROM system.parts WHERE table = 'flows_testnet' AND active"
