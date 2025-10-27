package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// PrometheusCollector implements MetricsCollector using Prometheus
type PrometheusCollector struct {
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
	pc := &PrometheusCollector{
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

	// Register metrics with Prometheus
	prometheus.MustRegister(
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
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	pc.logger.Info("starting metrics server",
		zap.String("addr", addr),
	)

	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			pc.logger.Error("metrics server error",
				zap.Error(err),
			)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	pc.logger.Info("shutting down metrics server")

	// Shutdown server with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}

// MockCollector implements MetricsCollector for testing
type MockCollector struct {
	ipChecksCount      int
	ipCheckErrorsCount int
	dnsUpdatesCount    map[string]int // "provider:record" -> count
	dnsErrorsCount     map[string]int // "provider:record" -> count
	currentIP          string
	lastChangeTime     time.Time
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
	m.ipChecksCount++
}

// IncrementIPCheckErrors increments the IP check errors counter
func (m *MockCollector) IncrementIPCheckErrors() {
	m.ipCheckErrorsCount++
}

// IncrementDNSUpdates increments the DNS updates counter
func (m *MockCollector) IncrementDNSUpdates(provider, record string) {
	key := provider + ":" + record
	m.dnsUpdatesCount[key]++
}

// IncrementDNSErrors increments the DNS update errors counter
func (m *MockCollector) IncrementDNSErrors(provider, record string) {
	key := provider + ":" + record
	m.dnsErrorsCount[key]++
}

// SetCurrentIP sets the current IP gauge
func (m *MockCollector) SetCurrentIP(ip string) {
	m.currentIP = ip
}

// SetLastChangeTime sets the last change timestamp
func (m *MockCollector) SetLastChangeTime(t time.Time) {
	m.lastChangeTime = t
}

// GetIPChecksCount returns the IP checks count
func (m *MockCollector) GetIPChecksCount() int {
	return m.ipChecksCount
}

// GetIPCheckErrorsCount returns the IP check errors count
func (m *MockCollector) GetIPCheckErrorsCount() int {
	return m.ipCheckErrorsCount
}

// GetDNSUpdatesCount returns the DNS updates count for a provider and record
func (m *MockCollector) GetDNSUpdatesCount(provider, record string) int {
	key := provider + ":" + record
	return m.dnsUpdatesCount[key]
}

// GetDNSErrorsCount returns the DNS errors count for a provider and record
func (m *MockCollector) GetDNSErrorsCount(provider, record string) int {
	key := provider + ":" + record
	return m.dnsErrorsCount[key]
}

// GetCurrentIP returns the current IP
func (m *MockCollector) GetCurrentIP() string {
	return m.currentIP
}

// GetLastChangeTime returns the last change time
func (m *MockCollector) GetLastChangeTime() time.Time {
	return m.lastChangeTime
}
