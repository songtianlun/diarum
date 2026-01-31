.PHONY: help build dev run clean test frontend backend docker version

# Get version from git
VERSION ?= $(shell git describe --dirty --always --tags --abbrev=7 2>/dev/null || echo "dev")
LDFLAGS := -X main.Version=$(VERSION)

# Default target
help:
	@echo "Diaria Development Commands:"
	@echo "  make build      - Build both frontend and backend"
	@echo "  make dev        - Run in development mode"
	@echo "  make run        - Run the application"
	@echo "  make frontend   - Build frontend only"
	@echo "  make backend    - Build backend only"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make test       - Run tests"
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
	go build -ldflags "$(LDFLAGS)" -o diaria .

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
	@echo "Starting backend server..."
	LOG_LEVEL=DEBUG go run . serve

# Run the built application
run:
	./diaria serve

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f diaria
	rm -rf site/build site/node_modules site/.svelte-kit
	rm -rf dist

# Run tests
test:
	go test ./...

# Build Docker image
docker:
	docker build -t diaria:latest .

# Install dependencies
deps:
	go mod download
	cd site && npm install
