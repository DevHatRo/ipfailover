package ipchecker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/devhat/ipfailover/pkg/errors"
	"go.uber.org/zap"
)

const (
	userAgent   = "ipfailover/1.0"
	maxBodySize = 4096 // 4KB limit for response body
)

// HTTPChecker implements IPChecker using HTTP endpoints
type HTTPChecker struct {
	client    *http.Client
	endpoints []string
	logger    *zap.Logger
}

// NewHTTPChecker creates a new HTTP-based IP checker
func NewHTTPChecker(endpoints []string, logger *zap.Logger) *HTTPChecker {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 5 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}

	return &HTTPChecker{
		client:    client,
		endpoints: endpoints,
		logger:    logger,
	}
}

// GetCurrentIP returns the current public IP address
func (h *HTTPChecker) GetCurrentIP(ctx context.Context) (string, error) {
	var lastErr error

	for i, endpoint := range h.endpoints {
		h.logger.Debug("checking IP endpoint",
			zap.String("endpoint", endpoint),
			zap.Int("attempt", i+1),
		)

		ip, err := h.checkEndpoint(ctx, endpoint)
		if err != nil {
			h.logger.Warn("IP check failed",
				zap.String("endpoint", endpoint),
				zap.Error(err),
			)
			lastErr = err
			continue
		}

		if ip != "" {
			h.logger.Info("IP check successful",
				zap.String("endpoint", endpoint),
				zap.String("ip", ip),
			)
			return ip, nil
		}
	}

	return "", errors.NewIPCheckError("all endpoints failed", lastErr)
}

// checkEndpoint checks a single endpoint for the current IP
func (h *HTTPChecker) checkEndpoint(ctx context.Context, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent to identify our requests
	req.Header.Set("User-Agent", userAgent)

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.NewHTTPError(resp.StatusCode, endpoint, fmt.Errorf("unexpected status code"))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check if we hit the size limit
	if len(body) == maxBodySize {
		return "", fmt.Errorf("response body exceeds maximum size limit of %d bytes", maxBodySize)
	}

	ip := strings.TrimSpace(string(body))
	if err := h.ValidateIP(ip); err != nil {
		return "", fmt.Errorf("invalid IP address: %w", err)
	}

	return ip, nil
}

// ValidateIP validates that the string is a valid IP address
func (h *HTTPChecker) ValidateIP(ip string) error {
	if ip == "" {
		return fmt.Errorf("empty IP address")
	}

	// Check if it's a valid IPv4 or IPv6 address
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid IP format: %s", ip)
	}

	return nil
}

// Name returns the checker name
func (h *HTTPChecker) Name() string {
	return "http"
}

// MockChecker implements IPChecker for testing
type MockChecker struct {
	ip  string
	err error
}

// NewMockChecker creates a new mock IP checker
func NewMockChecker(ip string, err error) *MockChecker {
	return &MockChecker{
		ip:  ip,
		err: err,
	}
}

// GetCurrentIP returns the mocked IP or error
func (m *MockChecker) GetCurrentIP(ctx context.Context) (string, error) {
	return m.ip, m.err
}

// Name returns the checker name
func (m *MockChecker) Name() string {
	return "mock"
}

// SetIP sets the IP to return (for testing)
func (m *MockChecker) SetIP(ip string) {
	m.ip = ip
}

// SetError sets the error to return (for testing)
func (m *MockChecker) SetError(err error) {
	m.err = err
}
