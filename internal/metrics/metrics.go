package metrics

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// PrometheusCollector implements MetricsCollector using Prometheus
type PrometheusCollector struct {
	registry           *prometheus.Registry
	ipChecksTotal      prometheus.Counter
	ipCheckErrorsTotal prometheus.Counter
	dnsUpdatesTotal    *prometheus.CounterVec
	dnsErrorsTotal     *prometheus.CounterVec
	currentIPGauge     *prometheus.GaugeVec
	lastChangeGauge    prometheus.Gauge
	logger             *zap.Logger
}

// NewPrometheusCollector creates a new Prometheus metrics collector
func NewPrometheusCollector(logger *zap.Logger) *PrometheusCollector {
	// Create a dedicated registry for this collector instance
	registry := prometheus.NewRegistry()

	pc := &PrometheusCollector{
		registry: registry,
		ipChecksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ipfailover_checks_total",
			Help: "Total number of IP checks performed",
		}),
		ipCheckErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ipfailover_check_errors_total",
			Help: "Total number of failed IP checks",
		}),
		dnsUpdatesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ipfailover_updates_total",
			Help: "Total number of DNS updates by provider and record",
		}, []string{"provider", "record"}),
		dnsErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ipfailover_update_errors_total",
			Help: "Total number of failed DNS updates by provider and record",
		}, []string{"provider", "record"}),
		currentIPGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ipfailover_current_ip_info",
			Help: "Current detected IP address",
		}, []string{"ip"}),
		lastChangeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ipfailover_last_change_timestamp_seconds",
			Help: "Timestamp of the last IP change",
		}),
		logger: logger,
	}

	// Register metrics with the dedicated registry
	registry.MustRegister(
		pc.ipChecksTotal,
		pc.ipCheckErrorsTotal,
		pc.dnsUpdatesTotal,
		pc.dnsErrorsTotal,
		pc.currentIPGauge,
		pc.lastChangeGauge,
	)

	return pc
}

// IncrementIPChecks increments the IP checks counter
func (pc *PrometheusCollector) IncrementIPChecks() {
	pc.ipChecksTotal.Inc()
	pc.logger.Debug("incremented IP checks counter")
}

// IncrementIPCheckErrors increments the IP check errors counter
func (pc *PrometheusCollector) IncrementIPCheckErrors() {
	pc.ipCheckErrorsTotal.Inc()
	pc.logger.Debug("incremented IP check errors counter")
}

// IncrementDNSUpdates increments the DNS updates counter
func (pc *PrometheusCollector) IncrementDNSUpdates(provider, record string) {
	pc.dnsUpdatesTotal.WithLabelValues(provider, record).Inc()
	pc.logger.Debug("incremented DNS updates counter",
		zap.String("provider", provider),
		zap.String("record", record),
	)
}

// IncrementDNSErrors increments the DNS update errors counter
func (pc *PrometheusCollector) IncrementDNSErrors(provider, record string) {
	pc.dnsErrorsTotal.WithLabelValues(provider, record).Inc()
	pc.logger.Debug("incremented DNS errors counter",
		zap.String("provider", provider),
		zap.String("record", record),
	)
}

// SetCurrentIP sets the current IP gauge
func (pc *PrometheusCollector) SetCurrentIP(ip string) {
	// Reset all labels first
	pc.currentIPGauge.Reset()

	// Set the new IP
	pc.currentIPGauge.WithLabelValues(ip).Set(1)
	pc.logger.Debug("set current IP gauge",
		zap.String("ip", ip),
	)
}

// SetLastChangeTime sets the last change timestamp
func (pc *PrometheusCollector) SetLastChangeTime(t time.Time) {
	pc.lastChangeGauge.Set(float64(t.Unix()))
	pc.logger.Debug("set last change timestamp",
		zap.Time("timestamp", t),
	)
}

// StartMetricsServer starts the Prometheus metrics HTTP server
func (pc *PrometheusCollector) StartMetricsServer(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(pc.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			pc.logger.Error("failed to write health response",
				zap.Error(err),
			)
			// Try to write error response if possible
			w.WriteHeader(http.StatusInternalServerError)
			if _, writeErr := w.Write([]byte("Internal Server Error")); writeErr != nil {
				pc.logger.Error("failed to write error response", zap.Error(writeErr))
			}
		}
	})

	// Create listener first to detect startup issues early
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		pc.logger.Error("failed to create listener",
			zap.String("addr", addr),
			zap.Error(err),
		)
		return err
	}

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	pc.logger.Info("starting metrics server",
		zap.String("addr", addr),
	)

	// Channel to receive server errors
	errCh := make(chan error, 1)

	// Start server in goroutine
	go func() {
		errCh <- server.Serve(listener)
	}()

	// Wait for context cancellation or server error
	select {
	case err := <-errCh:
		// Server error occurred
		if err != nil && err != http.ErrServerClosed {
			pc.logger.Error("metrics server error",
				zap.Error(err),
			)
			return err
		}
		return nil
	case <-ctx.Done():
		// Context cancelled, shutdown server
		pc.logger.Info("shutting down metrics server")

		// Shutdown server with timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			pc.logger.Error("failed to shutdown metrics server",
				zap.Error(err),
			)
			return err
		}

		// Wait for server goroutine to finish
		select {
		case err := <-errCh:
			if err != nil && err != http.ErrServerClosed {
				return err
			}
		case <-time.After(6 * time.Second):
			pc.logger.Warn("server shutdown timeout exceeded")
		}

		return nil
	}
}

// MockCollector implements MetricsCollector for testing
type MockCollector struct {
	mu                 sync.RWMutex
	ipChecksCount      int
	ipCheckErrorsCount int
	dnsUpdatesCount    map[string]int // "provider:record" -> count
	dnsErrorsCount     map[string]int // "provider:record" -> count
	currentIP          string
	lastChangeTime     time.Time
	// Note: Consider using a struct key type instead of "provider:record" string
	// to avoid potential delimiter collisions in provider/record names
}

// NewMockCollector creates a new mock metrics collector
func NewMockCollector() *MockCollector {
	return &MockCollector{
		dnsUpdatesCount: make(map[string]int),
		dnsErrorsCount:  make(map[string]int),
	}
}

// IncrementIPChecks increments the IP checks counter
func (m *MockCollector) IncrementIPChecks() {
	m.mu.Lock()
	m.ipChecksCount++
	m.mu.Unlock()
}

// IncrementIPCheckErrors increments the IP check errors counter
func (m *MockCollector) IncrementIPCheckErrors() {
	m.mu.Lock()
	m.ipCheckErrorsCount++
	m.mu.Unlock()
}

// IncrementDNSUpdates increments the DNS updates counter
func (m *MockCollector) IncrementDNSUpdates(provider, record string) {
	key := provider + ":" + record
	m.mu.Lock()
	m.dnsUpdatesCount[key]++
	m.mu.Unlock()
}

// IncrementDNSErrors increments the DNS update errors counter
func (m *MockCollector) IncrementDNSErrors(provider, record string) {
	key := provider + ":" + record
	m.mu.Lock()
	m.dnsErrorsCount[key]++
	m.mu.Unlock()
}

// SetCurrentIP sets the current IP gauge
func (m *MockCollector) SetCurrentIP(ip string) {
	m.mu.Lock()
	m.currentIP = ip
	m.mu.Unlock()
}

// SetLastChangeTime sets the last change timestamp
func (m *MockCollector) SetLastChangeTime(t time.Time) {
	m.mu.Lock()
	m.lastChangeTime = t
	m.mu.Unlock()
}

// GetIPChecksCount returns the IP checks count
func (m *MockCollector) GetIPChecksCount() int {
	m.mu.RLock()
	count := m.ipChecksCount
	m.mu.RUnlock()
	return count
}

// GetIPCheckErrorsCount returns the IP check errors count
func (m *MockCollector) GetIPCheckErrorsCount() int {
	m.mu.RLock()
	count := m.ipCheckErrorsCount
	m.mu.RUnlock()
	return count
}

// GetDNSUpdatesCount returns the DNS updates count for a provider and record
func (m *MockCollector) GetDNSUpdatesCount(provider, record string) int {
	key := provider + ":" + record
	m.mu.RLock()
	count := m.dnsUpdatesCount[key]
	m.mu.RUnlock()
	return count
}

// GetDNSErrorsCount returns the DNS errors count for a provider and record
func (m *MockCollector) GetDNSErrorsCount(provider, record string) int {
	key := provider + ":" + record
	m.mu.RLock()
	count := m.dnsErrorsCount[key]
	m.mu.RUnlock()
	return count
}

// GetCurrentIP returns the current IP
func (m *MockCollector) GetCurrentIP() string {
	m.mu.RLock()
	ip := m.currentIP
	m.mu.RUnlock()
	return ip
}

// GetLastChangeTime returns the last change time
func (m *MockCollector) GetLastChangeTime() time.Time {
	m.mu.RLock()
	t := m.lastChangeTime
	m.mu.RUnlock()
	return t
}
