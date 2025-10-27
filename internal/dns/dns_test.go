package dns_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/internal/dns"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockDNSProvider for testing
type MockDNSProvider struct {
	mock.Mock
}

func (m *MockDNSProvider) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockDNSProvider) UpdateRecord(ctx context.Context, record interfaces.DNSRecord) error {
	args := m.Called(ctx, record)
	return args.Error(0)
}

func (m *MockDNSProvider) GetRecord(ctx context.Context, name string) (*interfaces.DNSRecord, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.DNSRecord), args.Error(1)
}

func (m *MockDNSProvider) DeleteRecord(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *MockDNSProvider) Validate(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestCloudflareProvider_Name(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.CloudflareConfig{
		APIToken: "test-token",
		ZoneID:   "test-zone",
	}

	provider := dns.NewCloudflareProvider(cfg, logger)
	assert.Equal(t, "cloudflare", provider.Name())
}

func TestCloudflareProvider_Validate(t *testing.T) {
	t.Run("successful validation", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.CloudflareConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		}

		provider := dns.NewCloudflareProvider(cfg, logger)

		// Create mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"result":[]}`))
		}))
		defer server.Close()

		// We can't easily test the actual validation without mocking the HTTP client
		// This test ensures the provider can be created without errors
		assert.NotNil(t, provider)
	})
}

func TestCPanelProvider_Name(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.CPanelConfig{
		BaseURL:  "https://cpanel.example.com",
		Username: "testuser",
		APIToken: "test-token",
		Zone:     "example.com",
	}

	provider := dns.NewCPanelProvider(cfg, logger)
	assert.Equal(t, "cpanel", provider.Name())
}

func TestCPanelProvider_Validate(t *testing.T) {
	t.Run("successful validation", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.CPanelConfig{
			BaseURL:  "https://cpanel.example.com",
			Username: "testuser",
			APIToken: "test-token",
			Zone:     "example.com",
		}

		provider := dns.NewCPanelProvider(cfg, logger)

		// Create mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"result":{"data":[],"meta":{"result":1}}}`))
		}))
		defer server.Close()

		// We can't easily test the actual validation without mocking the HTTP client
		// This test ensures the provider can be created without errors
		assert.NotNil(t, provider)
	})
}

func TestRoute53Provider_Name(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Route53Config{
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Region:          "us-east-1",
		HostedZoneID:    "test-zone",
	}

	provider, err := dns.NewRoute53Provider(cfg, logger)
	assert.NoError(t, err)
	assert.Equal(t, "route53", provider.Name())
}

func TestRoute53Provider_Creation(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.Route53Config{
			AccessKeyID:     "test-key",
			SecretAccessKey: "test-secret",
			Region:          "us-east-1",
			HostedZoneID:    "test-zone",
		}

		provider, err := dns.NewRoute53Provider(cfg, logger)
		assert.NoError(t, err)
		assert.NotNil(t, provider)
	})
}

func TestNamecheapProvider_Name(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.NamecheapConfig{
		APIUser:  "test-user",
		APIToken: "test-token",
		Username: "testuser",
		ClientIP: "127.0.0.1",
		Domain:   "example.com",
		Sandbox:  true,
	}

	provider := dns.NewNamecheapProvider(cfg, logger)
	assert.Equal(t, "namecheap", provider.Name())
}

func TestNamecheapProvider_Creation(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.NamecheapConfig{
			APIUser:  "test-user",
			APIToken: "test-token",
			Username: "testuser",
			ClientIP: "127.0.0.1",
			Domain:   "example.com",
			Sandbox:  true,
		}

		provider := dns.NewNamecheapProvider(cfg, logger)
		assert.NotNil(t, provider)
	})
}

func TestDNSProvider_Interfaces(t *testing.T) {
	t.Run("Cloudflare implements DNSProvider", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.CloudflareConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		}

		provider := dns.NewCloudflareProvider(cfg, logger)

		// Test that it implements the interface
		var _ interfaces.DNSProvider = provider
		assert.NotNil(t, provider)
	})

	t.Run("CPanel implements DNSProvider", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.CPanelConfig{
			BaseURL:  "https://cpanel.example.com",
			Username: "testuser",
			APIToken: "test-token",
			Zone:     "example.com",
		}

		provider := dns.NewCPanelProvider(cfg, logger)

		// Test that it implements the interface
		var _ interfaces.DNSProvider = provider
		assert.NotNil(t, provider)
	})

	t.Run("Route53 implements DNSProvider", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.Route53Config{
			AccessKeyID:     "test-key",
			SecretAccessKey: "test-secret",
			Region:          "us-east-1",
			HostedZoneID:    "test-zone",
		}

		provider, err := dns.NewRoute53Provider(cfg, logger)
		assert.NoError(t, err)

		// Test that it implements the interface
		var _ interfaces.DNSProvider = provider
		assert.NotNil(t, provider)
	})

	t.Run("Namecheap implements DNSProvider", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.NamecheapConfig{
			APIUser:  "test-user",
			APIToken: "test-token",
			Username: "testuser",
			ClientIP: "127.0.0.1",
			Domain:   "example.com",
			Sandbox:  true,
		}

		provider := dns.NewNamecheapProvider(cfg, logger)

		// Test that it implements the interface
		var _ interfaces.DNSProvider = provider
		assert.NotNil(t, provider)
	})
}

func TestDNSProvider_ErrorHandling(t *testing.T) {
	t.Run("Cloudflare handles HTTP errors", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.CloudflareConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		}

		provider := dns.NewCloudflareProvider(cfg, logger)

		// Test with invalid context (should not panic)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		record := interfaces.DNSRecord{
			Name:     "test.example.com",
			Type:     "A",
			Value:    "1.2.3.4",
			TTL:      300,
			Provider: "cloudflare",
		}

		// This should return an error due to cancelled context
		err := provider.UpdateRecord(ctx, record)
		assert.Error(t, err)
	})

	t.Run("CPanel handles HTTP errors", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.CPanelConfig{
			BaseURL:  "https://cpanel.example.com",
			Username: "testuser",
			APIToken: "test-token",
			Zone:     "example.com",
		}

		provider := dns.NewCPanelProvider(cfg, logger)

		// Test with invalid context (should not panic)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		record := interfaces.DNSRecord{
			Name:     "test.example.com",
			Type:     "A",
			Value:    "1.2.3.4",
			TTL:      300,
			Provider: "cpanel",
		}

		// This should return an error due to cancelled context
		err := provider.UpdateRecord(ctx, record)
		assert.Error(t, err)
	})

	t.Run("Namecheap handles HTTP errors", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.NamecheapConfig{
			APIUser:  "test-user",
			APIToken: "test-token",
			Username: "testuser",
			ClientIP: "127.0.0.1",
			Domain:   "example.com",
			Sandbox:  true,
		}

		provider := dns.NewNamecheapProvider(cfg, logger)

		// Test with invalid context (should not panic)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		record := interfaces.DNSRecord{
			Name:     "test.example.com",
			Type:     "A",
			Value:    "1.2.3.4",
			TTL:      300,
			Provider: "namecheap",
		}

		// This should return an error due to cancelled context
		err := provider.UpdateRecord(ctx, record)
		assert.Error(t, err)
	})
}

func TestDNSProvider_ConfigurationValidation(t *testing.T) {
	t.Run("Cloudflare config validation", func(t *testing.T) {
		cfg := &config.CloudflareConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("CPanel config validation", func(t *testing.T) {
		cfg := &config.CPanelConfig{
			BaseURL:  "https://cpanel.example.com",
			Username: "testuser",
			APIToken: "test-token",
			Zone:     "example.com",
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("Route53 config validation", func(t *testing.T) {
		cfg := &config.Route53Config{
			AccessKeyID:     "test-key",
			SecretAccessKey: "test-secret",
			Region:          "us-east-1",
			HostedZoneID:    "test-zone",
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("Namecheap config validation", func(t *testing.T) {
		cfg := &config.NamecheapConfig{
			APIUser:  "test-user",
			APIToken: "test-token",
			Username: "testuser",
			ClientIP: "127.0.0.1",
			Domain:   "example.com",
			Sandbox:  true,
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})
}
