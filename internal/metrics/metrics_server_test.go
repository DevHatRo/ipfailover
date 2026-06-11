package metrics_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/devhat/ipfailover/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// freeAddr picks a free TCP address on 127.0.0.1 and returns it.
// There is a small TOCTOU race between closing the listener and the server
// binding it again, which is acceptable in tests.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func TestStartMetricsServer_ContextCancel_ReturnsNil(t *testing.T) {
	logger := zap.NewNop()
	collector := metrics.NewPrometheusCollector(logger)
	addr := freeAddr(t)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- collector.StartMetricsServer(ctx, addr)
	}()

	// Wait until the server is accepting connections.
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "server did not start in time")

	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("StartMetricsServer did not return after context cancel")
	}
}

func TestStartMetricsServer_ListenError_ReturnsError(t *testing.T) {
	logger := zap.NewNop()

	// Occupy a port first so the second bind fails.
	occupier, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupier.Close()

	addr := occupier.Addr().String()

	collector := metrics.NewPrometheusCollector(logger)
	err = collector.StartMetricsServer(context.Background(), addr)
	require.Error(t, err)
}

func TestStartMetricsServer_HealthEndpoint(t *testing.T) {
	logger := zap.NewNop()
	collector := metrics.NewPrometheusCollector(logger)
	addr := freeAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = collector.StartMetricsServer(ctx, addr)
	}()

	// Wait for server to be ready.
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "server did not start in time")

	resp, err := http.Get(fmt.Sprintf("http://%s/health", addr))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "OK", string(body))
}

func TestStartMetricsServer_MetricsEndpoint(t *testing.T) {
	logger := zap.NewNop()
	collector := metrics.NewPrometheusCollector(logger)
	addr := freeAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = collector.StartMetricsServer(ctx, addr)
	}()

	// Wait for server to be ready.
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "server did not start in time")

	// Increment a counter so there is something to scrape.
	collector.IncrementIPChecks()

	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", addr))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "ipfailover_checks_total")
}

func TestStartMetricsServer_MetricsValues(t *testing.T) {
	logger := zap.NewNop()
	collector := metrics.NewPrometheusCollector(logger)
	addr := freeAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = collector.StartMetricsServer(ctx, addr)
	}()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "server did not start in time")

	collector.IncrementIPChecks()
	collector.IncrementIPChecks()
	collector.IncrementIPCheckErrors()
	collector.IncrementDNSUpdates("cloudflare", "test.example.com")
	collector.IncrementDNSErrors("cloudflare", "test.example.com")
	collector.SetCurrentIP("10.0.0.1")
	collector.SetLastChangeTime(time.Now())

	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", addr))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	output := string(body)

	assert.Contains(t, output, "ipfailover_checks_total 2")
	assert.Contains(t, output, "ipfailover_check_errors_total 1")
	assert.Contains(t, output, `ipfailover_updates_total{provider="cloudflare",record="test.example.com"} 1`)
	assert.Contains(t, output, `ipfailover_update_errors_total{provider="cloudflare",record="test.example.com"} 1`)
	assert.Contains(t, output, `ipfailover_current_ip_info{ip="10.0.0.1"} 1`)
}
