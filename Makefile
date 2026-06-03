.PHONY: help build dev run clean test test-cover test-coverage frontend backend docker version install-air test-static

# Get version from git
VERSION ?= $(shell git describe --dirty --always --tags --abbrev=7 2>/dev/null || echo "dev")
LDFLAGS := -X main.Version=$(VERSION)
AIR_VERSION ?= latest
AIR := $(CURDIR)/.tmp/bin/air
GOCACHE ?= $(CURDIR)/.tmp/go-build
COVERAGE_FILE ?= $(CURDIR)/.tmp/coverage.out
COVERAGE_THRESHOLD ?= 80

# Default target
help:
	@echo "Diarum Development Commands:"
	@echo "  make build      - Build both frontend and backend"
	@echo "  make dev        - Run in development mode"
	@echo "  make run        - Run the application"
	@echo "  make frontend   - Build frontend only"
	@echo "  make backend    - Build backend only"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make test       - Run tests"
	@echo "  make test-cover - Run tests with coverage and enforce >= $(COVERAGE_THRESHOLD)%"
	@echo "  make docker     - Build Docker image"
	@echo "  make version    - Show current version"

# Show version
version:
	@echo "Version: $(VERSION)"

# Build everything
build: frontend backend

# Build frontend
frontend:
	@echo "Building frontend..."
	cd site && npm install && npm run build

# Build backend
backend: frontend
	@echo "Copying frontend build to embed location..."
	@mkdir -p internal/static/build
	@cp -r site/build/* internal/static/build/
	@echo "Building backend with version $(VERSION)..."
	go build -ldflags "$(LDFLAGS)" -o diarum .

# Development mode (requires running frontend and backend separately)
dev:
	@echo "Starting development mode..."
	@echo "Run 'make dev-frontend' in one terminal and 'make dev-backend' in another"

dev-frontend:
	@echo "Installing frontend dependencies..."
	@cd site && npm install
	@echo "Starting frontend dev server..."
	cd site && npm run dev

dev-backend:
	@echo "Installing backend dependencies..."
	@go mod download
	@mkdir -p .tmp
	@$(MAKE) install-air
	@echo "Starting backend server with air..."
	exec $(AIR) -c .air.toml

install-air: $(AIR)

$(AIR):
	@echo "Installing air..."
	@mkdir -p .tmp/bin
	@GOBIN=$(CURDIR)/.tmp/bin go install github.com/air-verse/air@$(AIR_VERSION)

# Run the built application
run:
	./diarum serve

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f diarum
	rm -rf .tmp
	rm -rf site/build site/node_modules site/.svelte-kit
	rm -rf dist

# Run tests
test-static:
	@mkdir -p internal/static/build
	@test -f internal/static/build/index.html || printf '<!doctype html><title>Diarum test static</title>\n' > internal/static/build/index.html

test: test-static
	@mkdir -p $(GOCACHE)
	GOCACHE=$(GOCACHE) go test ./...

test-cover test-coverage: test-static
	@mkdir -p $(dir $(COVERAGE_FILE)) $(GOCACHE)
	GOCACHE=$(GOCACHE) go test -coverprofile=$(COVERAGE_FILE) ./...
	GOCACHE=$(GOCACHE) go tool cover -func=$(COVERAGE_FILE)
	@coverage=$$(GOCACHE=$(GOCACHE) go tool cover -func=$(COVERAGE_FILE) | awk '/^total:/ {gsub("%","",$$3); print $$3}'); \
	awk "BEGIN { exit !($$coverage >= $(COVERAGE_THRESHOLD)) }" || (echo "Coverage $$coverage% is below $(COVERAGE_THRESHOLD)%" && exit 1)

# Build Docker image
docker:
	docker build -t diarum:latest .

# Install dependencies
deps:
	go mod download
	cd site && npm install
