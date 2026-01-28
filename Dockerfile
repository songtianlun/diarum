# Build stage for frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/site
COPY site/package*.json ./
RUN npm install
COPY site/ ./
RUN npm run build

# Build stage for backend
FROM golang:1.22-alpine AS backend-builder
WORKDIR /app

# Install git for version detection
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Get version from build arg or git
ARG VERSION
RUN if [ -z "$VERSION" ]; then \
      VERSION=$(git describe --dirty --always --tags --abbrev=7 2>/dev/null || echo "docker"); \
    fi && \
    echo "Building version: $VERSION" && \
    go build -ldflags "-X main.Version=$VERSION" -o diaria .

# Final stage
FROM alpine:latest
WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Copy binary from backend builder
COPY --from=backend-builder /app/diaria /app/diaria

# Copy frontend build from frontend builder
COPY --from=frontend-builder /app/site/build /app/site/build

# Create pb_data directory
RUN mkdir -p /app/pb_data

# Expose port
EXPOSE 8090

# Run the application
CMD ["/app/diaria", "serve", "--http=0.0.0.0:8090"]
