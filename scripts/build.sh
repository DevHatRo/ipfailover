#!/bin/bash

set -e

# Create bin directory if it doesn't exist
mkdir -p ./bin

echo "Building IP Failover binaries..."

# Get version information
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

echo "Version: $VERSION"
echo "Build time: $BUILD_TIME"

# Build main application for multiple platforms
# CGO_ENABLED=0 ensures static linking (required for distroless/alpine containers)
echo "Building main application binaries..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" -o ./bin/ipfailover-linux-amd64 ./cmd/ipfailover
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" -o ./bin/ipfailover-linux-arm64 ./cmd/ipfailover
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-w -s -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" -o ./bin/ipfailover-darwin-amd64 ./cmd/ipfailover
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-w -s -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" -o ./bin/ipfailover-darwin-arm64 ./cmd/ipfailover
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-w -s -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" -o ./bin/ipfailover-windows-amd64.exe ./cmd/ipfailover

# Make binaries executable
chmod +x ./bin/*

echo "Build completed successfully!"
echo "Binaries available in ./bin/"
echo ""
echo "Available binaries:"
ls -la ./bin/
