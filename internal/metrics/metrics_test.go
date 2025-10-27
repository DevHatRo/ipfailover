package metrics_test

import (
	"testing"
	"time"

	"github.com/devhat/ipfailover/internal/metrics"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestPrometheusCollector(t *testing.T) {
	logger := zap.NewNop()
	collector := metrics.NewPrometheusCollector(logger)

	// Test all methods
	collector.IncrementIPChecks()
	collector.IncrementIPChecks()
	collector.IncrementIPCheckErrors()
	collector.IncrementDNSUpdates("cloudflare", "example.com")
	collector.IncrementDNSErrors("cloudflare", "example.com")
	collector.SetCurrentIP("203.0.113.10")
	collector.SetLastChangeTime(time.Now())

	// Test that metrics are registered (we can't easily test the actual values without
	// starting a metrics server, but we can ensure no panics occur)
	assert.NotNil(t, collector)
}

func TestMockCollector(t *testing.T) {
	collector := metrics.NewMockCollector()

	t.Run("IncrementIPChecks", func(t *testing.T) {
		collector.IncrementIPChecks()
		collector.IncrementIPChecks()

		assert.Equal(t, 2, collector.GetIPChecksCount())
	})

	t.Run("IncrementIPCheckErrors", func(t *testing.T) {
		collector.IncrementIPCheckErrors()
		collector.IncrementIPCheckErrors()
		collector.IncrementIPCheckErrors()

		assert.Equal(t, 3, collector.GetIPCheckErrorsCount())
	})

	t.Run("IncrementDNSUpdates", func(t *testing.T) {
		collector.IncrementDNSUpdates("cloudflare", "example.com")
		collector.IncrementDNSUpdates("cloudflare", "api.example.com")
		collector.IncrementDNSUpdates("cpanel", "backup.example.com")

		assert.Equal(t, 1, collector.GetDNSUpdatesCount("cloudflare", "example.com"))
		assert.Equal(t, 1, collector.GetDNSUpdatesCount("cloudflare", "api.example.com"))
		assert.Equal(t, 1, collector.GetDNSUpdatesCount("cpanel", "backup.example.com"))
	})

	t.Run("IncrementDNSErrors", func(t *testing.T) {
		collector.IncrementDNSErrors("cloudflare", "example.com")
		collector.IncrementDNSErrors("cloudflare", "example.com")

		assert.Equal(t, 2, collector.GetDNSErrorsCount("cloudflare", "example.com"))
	})

	t.Run("SetCurrentIP", func(t *testing.T) {
		collector.SetCurrentIP("203.0.113.10")
		assert.Equal(t, "203.0.113.10", collector.GetCurrentIP())

		collector.SetCurrentIP("198.51.100.77")
		assert.Equal(t, "198.51.100.77", collector.GetCurrentIP())
	})

	t.Run("SetLastChangeTime", func(t *testing.T) {
		now := time.Now()
		collector.SetLastChangeTime(now)

		actualTime := collector.GetLastChangeTime()
		assert.Equal(t, now, actualTime)
	})
}

func TestMockCollector_InitialState(t *testing.T) {
	collector := metrics.NewMockCollector()

	assert.Equal(t, 0, collector.GetIPChecksCount())
	assert.Equal(t, 0, collector.GetIPCheckErrorsCount())
	assert.Equal(t, 0, collector.GetDNSUpdatesCount("cloudflare", "example.com"))
	assert.Equal(t, 0, collector.GetDNSErrorsCount("cloudflare", "example.com"))
	assert.Empty(t, collector.GetCurrentIP())
	assert.Zero(t, collector.GetLastChangeTime())
}
