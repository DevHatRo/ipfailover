package app

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- Test mocks ---

type mockIPChecker struct {
	ip  string
	err error
}

func (m *mockIPChecker) GetCurrentIP(ctx context.Context) (string, error) { return m.ip, m.err }
func (m *mockIPChecker) Name() string                                     { return "mock" }

type mockReachability struct {
	mu          sync.Mutex
	reachable   map[string]bool
	defaultResp error
}

func newMockReachability() *mockReachability {
	return &mockReachability{reachable: make(map[string]bool)}
}

func (m *mockReachability) SetReachable(ip string, reachable bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reachable[ip] = reachable
}

func (m *mockReachability) CheckReachability(ctx context.Context, ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.reachable[ip]; ok && r {
		return nil
	}
	if m.defaultResp != nil {
		return m.defaultResp
	}
	return fmt.Errorf("unreachable: %s", ip)
}

type mockStateStore struct {
	mu                  sync.Mutex
	lastAppliedIP       string
	lastChangeTime      time.Time
	lastCheckIP         string
	lastCheckTime       time.Time
	primaryFailureCount int
	failOnGet           bool
	failOnSet           bool
}

func newMockStateStore() *mockStateStore { return &mockStateStore{} }

func (m *mockStateStore) GetLastAppliedIP(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastAppliedIP == "" {
		return "", errors.NewNotFoundError("state", fmt.Errorf("not found"))
	}
	return m.lastAppliedIP, nil
}

func (m *mockStateStore) SetLastAppliedIP(ctx context.Context, ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOnSet {
		return fmt.Errorf("mock set failure")
	}
	m.lastAppliedIP = ip
	m.lastChangeTime = time.Now()
	return nil
}

func (m *mockStateStore) GetLastChangeTime(ctx context.Context) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastChangeTime, nil
}

func (m *mockStateStore) SetLastChangeTime(ctx context.Context, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastChangeTime = t
	return nil
}

func (m *mockStateStore) SetLastCheckInfo(ctx context.Context, ip string, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCheckIP = ip
	m.lastCheckTime = t
	return nil
}

func (m *mockStateStore) GetLastCheckInfo(ctx context.Context) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastCheckIP, m.lastCheckTime, nil
}

func (m *mockStateStore) GetPrimaryFailureCount(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOnGet {
		return 0, fmt.Errorf("mock get failure")
	}
	return m.primaryFailureCount, nil
}

func (m *mockStateStore) SetPrimaryFailureCount(ctx context.Context, count int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOnSet {
		return fmt.Errorf("mock set failure")
	}
	m.primaryFailureCount = count
	return nil
}

func (m *mockStateStore) ResetPrimaryFailureCount(ctx context.Context) error {
	return m.SetPrimaryFailureCount(ctx, 0)
}

type mockMetrics struct {
	mu               sync.Mutex
	ipChecks         int
	ipCheckErrors    int
	dnsUpdates       int
	dnsErrors        int
	currentIP        string
	lastChangeTime   time.Time
	metricsServerErr error
}

func newMockMetrics() *mockMetrics { return &mockMetrics{} }

func (m *mockMetrics) IncrementIPChecks() {
	m.mu.Lock()
	m.ipChecks++
	m.mu.Unlock()
}
func (m *mockMetrics) IncrementIPCheckErrors() {
	m.mu.Lock()
	m.ipCheckErrors++
	m.mu.Unlock()
}
func (m *mockMetrics) IncrementDNSUpdates(provider, record string) {
	m.mu.Lock()
	m.dnsUpdates++
	m.mu.Unlock()
}
func (m *mockMetrics) IncrementDNSErrors(provider, record string) {
	m.mu.Lock()
	m.dnsErrors++
	m.mu.Unlock()
}
func (m *mockMetrics) SetCurrentIP(ip string) {
	m.mu.Lock()
	m.currentIP = ip
	m.mu.Unlock()
}
func (m *mockMetrics) SetLastChangeTime(t time.Time) {
	m.mu.Lock()
	m.lastChangeTime = t
	m.mu.Unlock()
}
func (m *mockMetrics) StartMetricsServer(ctx context.Context, addr string) error {
	if m.metricsServerErr != nil {
		return m.metricsServerErr
	}
	<-ctx.Done()
	return ctx.Err()
}

type mockDNSProvider struct {
	name      string
	updateErr error
	records   map[string]*interfaces.DNSRecord
}

func newMockDNSProvider(name string) *mockDNSProvider {
	return &mockDNSProvider{name: name, records: make(map[string]*interfaces.DNSRecord)}
}

type mockNotifier struct {
	events []interfaces.FailoverEvent
}

func (m *mockNotifier) Notify(ctx context.Context, event interfaces.FailoverEvent) error {
	m.events = append(m.events, event)
	return nil
}
func (m *mockNotifier) Name() string { return "mock" }

func (m *mockDNSProvider) Name() string { return m.name }
func (m *mockDNSProvider) UpdateRecord(ctx context.Context, record interfaces.DNSRecord) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.records[record.Name] = &record
	return nil
}
func (m *mockDNSProvider) GetRecord(ctx context.Context, name, rtype string) (*interfaces.DNSRecord, error) {
	r, ok := m.records[name]
	if !ok {
		return nil, nil
	}
	return r, nil
}
func (m *mockDNSProvider) DeleteRecord(ctx context.Context, name, recordType string) error {
	delete(m.records, name)
	return nil
}
func (m *mockDNSProvider) Validate(ctx context.Context) error { return nil }

// --- Helper ---

func newTestApp(opts ...func(*Application)) *Application {
	logger := zap.NewNop()
	cfg := &config.Config{
		PollInterval:         30 * time.Second,
		PrimaryIP:            "10.0.0.1",
		SecondaryIP:          "10.0.0.2",
		FailoverRetries:      3,
		StateFailureStrategy: "continue_with_warning",
		StateFile:            "/tmp/test-state.json",
		MetricsAddr:          ":0",
		LogLevel:             "debug",
		CheckEndpoints:       []string{"https://ifconfig.io/ip"},
		DNS: []config.DNSConfig{
			{
				Name:     "test.example.com",
				Type:     "A",
				Provider: "mock",
				TTL:      300,
			},
		},
	}

	reachability := newMockReachability()
	reachability.SetReachable("10.0.0.1", true)

	provider := newMockDNSProvider("mock")

	a := &Application{
		Config:              cfg,
		Logger:              logger,
		IPChecker:           &mockIPChecker{ip: "10.0.0.1"},
		DNSProviders:        map[string]interfaces.DNSProvider{"test.example.com": provider},
		StateStore:          newMockStateStore(),
		Metrics:             newMockMetrics(),
		ReachabilityChecker: reachability,
		Notifier:            &mockNotifier{},
	}

	for _, opt := range opts {
		opt(a)
	}
	return a
}

// --- Tests ---

func TestDetermineTargetIP_PrimaryReachable(t *testing.T) {
	a := newTestApp()

	ip, err := a.DetermineTargetIP(context.Background(), "10.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", ip)
}

func TestDetermineTargetIP_PrimaryUnreachable_ExceedsRetries(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.Config.FailoverRetries = 2
		r := newMockReachability()
		r.SetReachable("10.0.0.1", false)
		a.ReachabilityChecker = r
		s := newMockStateStore()
		s.primaryFailureCount = 1
		a.StateStore = s
	})

	ip, err := a.DetermineTargetIP(context.Background(), "10.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", ip)
}

func TestDetermineTargetIP_PrimaryUnreachable_WithinRetries(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.Config.FailoverRetries = 5
		r := newMockReachability()
		r.SetReachable("10.0.0.1", false)
		a.ReachabilityChecker = r
	})

	ip, err := a.DetermineTargetIP(context.Background(), "10.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", ip)
}

func TestDetermineTargetIP_FirstRun_PrimaryUnreachable_SecondaryReachable(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.Config.FailoverRetries = 5
		r := newMockReachability()
		r.SetReachable("10.0.0.1", false)
		r.SetReachable("10.0.0.2", true)
		a.ReachabilityChecker = r
	})

	ip, err := a.DetermineTargetIP(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", ip)
}

func TestDetermineTargetIP_FirstRun_BothUnreachable(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.Config.FailoverRetries = 5
		r := newMockReachability()
		r.SetReachable("10.0.0.1", false)
		r.SetReachable("10.0.0.2", false)
		a.ReachabilityChecker = r
	})

	ip, err := a.DetermineTargetIP(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "", ip)
}

func TestDetermineTargetIP_FailFast_StateGetFailure(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.Config.StateFailureStrategy = "fail_fast"
		r := newMockReachability()
		r.SetReachable("10.0.0.1", false)
		a.ReachabilityChecker = r
		s := newMockStateStore()
		s.failOnGet = true
		a.StateStore = s
	})

	_, err := a.DetermineTargetIP(context.Background(), "10.0.0.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state persistence failure")
}

func TestDetermineTargetIP_ImmediateFailover_StateGetFailure(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.Config.StateFailureStrategy = "immediate_failover"
		r := newMockReachability()
		r.SetReachable("10.0.0.1", false)
		a.ReachabilityChecker = r
		s := newMockStateStore()
		s.failOnGet = true
		a.StateStore = s
	})

	ip, err := a.DetermineTargetIP(context.Background(), "10.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", ip)
}

func TestDetermineTargetIP_TransientFailureCount(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.Config.FailoverRetries = 3
		r := newMockReachability()
		r.SetReachable("10.0.0.1", false)
		a.ReachabilityChecker = r
		s := newMockStateStore()
		s.failOnSet = true
		a.StateStore = s
	})

	// First call: Set fails, transient=1, total=1+1=2, within retries -> primary
	ip, err := a.DetermineTargetIP(context.Background(), "10.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", ip)
	assert.Equal(t, 1, a.TransientFailureCount)

	// Second call: Set fails again, transient=2, total=1+2=3, exceeds retries -> secondary
	ip, err = a.DetermineTargetIP(context.Background(), "10.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", ip)
}

func TestCheckAndUpdateIP_NoChange(t *testing.T) {
	ss := newMockStateStore()
	ss.lastAppliedIP = "10.0.0.1"

	a := newTestApp(func(a *Application) {
		a.StateStore = ss
	})

	err := a.CheckAndUpdateIP(context.Background())
	require.NoError(t, err)
}

func TestCheckAndUpdateIP_IPChanged(t *testing.T) {
	ss := newMockStateStore()
	ss.lastAppliedIP = "10.0.0.99"

	provider := newMockDNSProvider("mock")

	a := newTestApp(func(a *Application) {
		a.StateStore = ss
		a.DNSProviders = map[string]interfaces.DNSProvider{"test.example.com": provider}
	})

	err := a.CheckAndUpdateIP(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "10.0.0.1", ss.lastAppliedIP)

	rec := provider.records["test.example.com"]
	require.NotNil(t, rec)
	assert.Equal(t, "10.0.0.1", rec.Value)
}

func TestCheckAndUpdateIP_IPCheckFails(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.IPChecker = &mockIPChecker{err: fmt.Errorf("network down")}
	})

	err := a.CheckAndUpdateIP(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network down")
}

func TestUpdateDNSRecords_ProviderNotFound(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.DNSProviders = map[string]interfaces.DNSProvider{}
	})

	err := a.UpdateDNSRecords(context.Background(), "10.0.0.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DNS provider not found")
}

func TestUpdateDNSRecords_ProviderError(t *testing.T) {
	provider := newMockDNSProvider("mock")
	provider.updateErr = fmt.Errorf("API error")

	a := newTestApp(func(a *Application) {
		a.DNSProviders = map[string]interfaces.DNSProvider{"test.example.com": provider}
	})

	err := a.UpdateDNSRecords(context.Background(), "10.0.0.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
}

func TestHealthCheck_Success(t *testing.T) {
	ss := newMockStateStore()
	ss.lastAppliedIP = "10.0.0.1"

	a := newTestApp(func(a *Application) {
		a.StateStore = ss
	})

	err := a.HealthCheck()
	require.NoError(t, err)
}

func TestHealthCheck_IPCheckFails(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.IPChecker = &mockIPChecker{err: fmt.Errorf("timeout")}
	})

	err := a.HealthCheck()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IP check failed")
}

func TestCheckAndUpdateIP_PartialDNSFailure_DoesNotPersistState(t *testing.T) {
	okProvider := newMockDNSProvider("ok")
	failProvider := newMockDNSProvider("fail")
	failProvider.updateErr = fmt.Errorf("API error")

	ss := newMockStateStore()
	ss.lastAppliedIP = "10.0.0.1"

	reachability := newMockReachability()
	reachability.SetReachable("10.0.0.2", true)

	a := newTestApp(func(a *Application) {
		a.Config.DNS = []config.DNSConfig{
			{Name: "ok.example.com", Type: "A", Provider: "mock", TTL: 300},
			{Name: "fail.example.com", Type: "A", Provider: "mock", TTL: 300},
		}
		a.DNSProviders = map[string]interfaces.DNSProvider{
			"ok.example.com":   okProvider,
			"fail.example.com": failProvider,
		}
		a.StateStore = ss
		a.Config.FailoverRetries = 1
		a.ReachabilityChecker = reachability
	})

	err := a.CheckAndUpdateIP(context.Background())
	require.Error(t, err)

	// One provider succeeded, but state must not record the new IP so the
	// next poll retries all providers.
	lastApplied, err := ss.GetLastAppliedIP(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", lastApplied)
}

func TestReloadConfig_AppliesMutableFields(t *testing.T) {
	a := NewApplication(
		newTestApp().Config,
		zap.NewNop(),
		&mockIPChecker{ip: "10.0.0.1"},
		map[string]interfaces.DNSProvider{},
		newMockStateStore(),
		newMockMetrics(),
		newMockReachability(),
		&mockNotifier{},
	)

	newCfg := &config.Config{
		PollInterval:         10 * time.Second,
		FailoverRetries:      7,
		PrimaryIP:            "10.1.0.1",
		SecondaryIP:          "10.1.0.2",
		StateFailureStrategy: "fail_fast",
		ReachabilityPort:     "443",
		ReachabilityTimeout:  9 * time.Second,
		// Immutable fields must be ignored by reload
		MetricsAddr: ":9999",
	}
	a.ReloadConfig(newCfg)

	got := a.Cfg()
	assert.Equal(t, 10*time.Second, got.PollInterval)
	assert.Equal(t, 7, got.FailoverRetries)
	assert.Equal(t, "10.1.0.1", got.PrimaryIP)
	assert.Equal(t, "10.1.0.2", got.SecondaryIP)
	assert.Equal(t, "fail_fast", got.StateFailureStrategy)
	assert.Equal(t, "443", got.ReachabilityPort)
	assert.Equal(t, 9*time.Second, got.ReachabilityTimeout)
	assert.Equal(t, ":0", got.MetricsAddr, "non-mutable fields must be preserved")

	// Reload signal should be queued for the run loop
	select {
	case <-a.reloadCh:
	default:
		t.Fatal("expected reload signal on reloadCh")
	}
}

func TestReloadConfig_ConcurrentWithReads(t *testing.T) {
	a := newTestApp(func(a *Application) {
		a.reloadCh = make(chan struct{}, 1)
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			newCfg := *a.Cfg()
			newCfg.PrimaryIP = fmt.Sprintf("10.0.%d.1", i%250)
			a.ReloadConfig(&newCfg)
		}
		close(stop)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = a.DetermineTargetIP(context.Background(), "10.0.0.1")
			}
		}
	}()

	wg.Wait()
}
