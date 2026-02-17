package app

import (
	"context"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
)

// TCPReachabilityChecker checks IP reachability via TCP connection
type TCPReachabilityChecker struct {
	Port    string
	Timeout time.Duration
	Logger  *zap.Logger
}

// NewTCPReachabilityChecker creates a new TCP reachability checker with configurable port and timeout
func NewTCPReachabilityChecker(port string, timeout time.Duration, logger *zap.Logger) *TCPReachabilityChecker {
	if port == "" {
		port = "80"
	}
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	return &TCPReachabilityChecker{
		Port:    port,
		Timeout: timeout,
		Logger:  logger,
	}
}

// CheckReachability attempts to verify connectivity to the given IP address
func (c *TCPReachabilityChecker) CheckReachability(ctx context.Context, ip string) error {
	addr := net.JoinHostPort(ip, c.Port)
	conn, err := net.DialTimeout("tcp", addr, c.Timeout)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			c.Logger.Debug("failed to close connection", zap.Error(closeErr))
		}
	}()

	return nil
}
