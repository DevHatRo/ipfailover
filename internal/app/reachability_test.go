package app

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewTCPReachabilityChecker_Defaults(t *testing.T) {
	logger := zap.NewNop()

	t.Run("empty port defaults to 80", func(t *testing.T) {
		c := NewTCPReachabilityChecker("", 0, logger)
		assert.Equal(t, "80", c.Port)
		assert.Equal(t, 3*time.Second, c.Timeout)
		assert.Equal(t, logger, c.Logger)
	})

	t.Run("zero timeout defaults to 3s", func(t *testing.T) {
		c := NewTCPReachabilityChecker("8080", 0, logger)
		assert.Equal(t, "8080", c.Port)
		assert.Equal(t, 3*time.Second, c.Timeout)
	})

	t.Run("explicit values are preserved", func(t *testing.T) {
		c := NewTCPReachabilityChecker("443", 5*time.Second, logger)
		assert.Equal(t, "443", c.Port)
		assert.Equal(t, 5*time.Second, c.Timeout)
		assert.Equal(t, logger, c.Logger)
	})
}

func TestCheckReachability_Success(t *testing.T) {
	// Start a real TCP listener on a random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	// Accept connections in the background so the dial does not hang.
	go func() {
		for {
			c, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			c.Close()
		}
	}()

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	logger := zap.NewNop()
	checker := NewTCPReachabilityChecker(portStr, time.Second, logger)

	err = checker.CheckReachability(context.Background(), "127.0.0.1")
	assert.NoError(t, err)
}

func TestCheckReachability_ClosedPort_ReturnsError(t *testing.T) {
	// Grab a free port then close the listener so nothing is listening there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	logger := zap.NewNop()
	checker := &TCPReachabilityChecker{
		Port:    fmt.Sprintf("%d", port),
		Timeout: 200 * time.Millisecond,
		Logger:  logger,
	}

	err = checker.CheckReachability(context.Background(), "127.0.0.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to")
}

func TestCheckReachability_CanceledContext_ReturnsError(t *testing.T) {
	// Use TCP port 9 (discard) on a documentation/test address so the
	// connection attempt is unlikely to succeed before the cancel fires.
	// The timeout is generous to avoid a flaky test on very fast machines.
	logger := zap.NewNop()
	checker := NewTCPReachabilityChecker("9", 5*time.Second, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before dialing

	err := checker.CheckReachability(ctx, "192.0.2.1")
	assert.Error(t, err)
}
