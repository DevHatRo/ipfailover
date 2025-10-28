package dns_test

import (
	"context"
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

func (m *MockDNSProvider) GetRecord(ctx context.Context, name string, rtype string) (*interfaces.DNSRecord, error) {
	args := m.Called(ctx, name, rtype)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.DNSRecord), args.Error(1)
}

func (m *MockDNSProvider) DeleteRecord(ctx context.Context, name, recordType string) error {
	args := m.Called(ctx, name, recordType)
	return args.Error(0)
}

func (m *MockDNSProvider) Validate(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
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

	t.Run("Hetzner implements DNSProvider", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.HetznerConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		}

		provider := dns.NewHetznerProvider(cfg, logger)

		// Test that it implements the interface
		var _ interfaces.DNSProvider = provider
		assert.NotNil(t, provider)
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

	t.Run("Hetzner config validation", func(t *testing.T) {
		cfg := &config.HetznerConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("Hetzner config validation - missing API token", func(t *testing.T) {
		cfg := &config.HetznerConfig{
			ZoneID: "test-zone",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api_token is required")
	})

	t.Run("Hetzner config validation - missing zone ID", func(t *testing.T) {
		cfg := &config.HetznerConfig{
			APIToken: "test-token",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "zone_id is required")
	})
}
