# Makefile for IP Failover

.PHONY: build test test-coverage clean docker-build docker-run

# Variables
BINARY_NAME=bin/ipfailover
IMAGE_NAME=ipfailover
TAG=$(shell git describe --tags --always --dirty)
VERSION=$(TAG)
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/ipfailover

# Build binaries for all platforms
build-all:
	@echo "Building binaries for all platforms..."
	./scripts/build.sh

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -cover ./...
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests with coverage threshold check
test-coverage-check:
	@echo "Running tests with coverage threshold check..."
	go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | grep total | awk '{coverage=$$3; gsub(/%/, "", coverage); if (coverage < 60) {print "Coverage " coverage "% is below 60% threshold"; exit 1} else {print "Coverage " coverage "% meets 60% threshold"; exit 0}}'

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -rf bin/

fmt:
	@echo "Formatting code..."
	go fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	go test -bench=. ./...

# Generate mocks (if using mockgen)
mocks:
	@echo "Generating mocks..."
	go generate ./...

# Show help
help:
	@echo "Available targets:"
	@echo "  build              - Build the binary"
	@echo "  build-all          - Build binaries for all platforms"
	@echo "  test               - Run tests"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  test-coverage-check - Run tests with coverage threshold check"
	@echo "  clean              - Clean build artifacts"
	@echo "  docker-build       - Build Docker image"
	@echo "  docker-build-all   - Build Docker images for all platforms"
	@echo "  docker-run         - Run Docker container"
	@echo "  fmt                - Format code"
	@echo "  lint               - Lint code"
	@echo "  bench              - Run benchmarks"
	@echo "  mocks              - Generate mocks"
	@echo "  help               - Show this help"
