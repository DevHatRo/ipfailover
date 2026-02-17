package metrics

import (
	"context"
	"sync"
	"time"
)

// MockCollector implements MetricsCollector for testing
type MockCollector struct {
	mu                 sync.RWMutex
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

// StartMetricsServer is a no-op for the mock collector
func (m *MockCollector) StartMetricsServer(ctx context.Context, addr string) error {
	<-ctx.Done()
	return ctx.Err()
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
