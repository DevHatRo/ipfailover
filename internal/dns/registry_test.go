package dns_test

import (
	"sort"
	"testing"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/internal/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRegisteredProviders(t *testing.T) {
	providers := dns.RegisteredProviders()
	sort.Strings(providers)

	expected := []string{"cloudflare", "cpanel", "hetzner", "route53"}
	sort.Strings(expected)

	assert.Equal(t, expected, providers)
}

func TestIsRegisteredProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     bool
	}{
		{"cloudflare is registered", "cloudflare", true},
		{"cpanel is registered", "cpanel", true},
		{"route53 is registered", "route53", true},
		{"hetzner is registered", "hetzner", true},
		{"unknown is not registered", "unknown", false},
		{"empty string is not registered", "", false},
		{"CLOUDFLARE (uppercase) is not registered", "CLOUDFLARE", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, dns.IsRegisteredProvider(tt.provider))
		})
	}
}

func TestCreateProvider_UnknownProvider(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider: "nonexistent",
	}

	provider, err := dns.CreateProvider(cfg, logger)
	require.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "unsupported DNS provider")
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestCreateProvider_Cloudflare_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider:   "cloudflare",
		Cloudflare: nil,
	}

	provider, err := dns.CreateProvider(cfg, logger)
	require.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "cloudflare")
}

func TestCreateProvider_Cloudflare_ValidConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider: "cloudflare",
		Cloudflare: &config.CloudflareConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		},
	}

	provider, err := dns.CreateProvider(cfg, logger)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "cloudflare", provider.Name())
}

func TestCreateProvider_CPanel_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider: "cpanel",
		CPanel:   nil,
	}

	provider, err := dns.CreateProvider(cfg, logger)
	require.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "cpanel")
}

func TestCreateProvider_CPanel_ValidConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider: "cpanel",
		CPanel: &config.CPanelConfig{
			BaseURL:  "https://cpanel.example.com",
			Username: "user",
			APIToken: "tok",
			Zone:     "example.com",
		},
	}

	provider, err := dns.CreateProvider(cfg, logger)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "cpanel", provider.Name())
}

func TestCreateProvider_Route53_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider: "route53",
		Route53:  nil,
	}

	provider, err := dns.CreateProvider(cfg, logger)
	require.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "route53")
}

func TestCreateProvider_Route53_ValidConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider: "route53",
		Route53: &config.Route53Config{
			AccessKeyID:     "ak",
			SecretAccessKey: "sk",
			Region:          "us-east-1",
			HostedZoneID:    "hz",
		},
	}

	provider, err := dns.CreateProvider(cfg, logger)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "route53", provider.Name())
}

func TestCreateProvider_Hetzner_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider: "hetzner",
		Hetzner:  nil,
	}

	provider, err := dns.CreateProvider(cfg, logger)
	require.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "hetzner")
}

func TestCreateProvider_Hetzner_ValidConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider: "hetzner",
		Hetzner: &config.HetznerConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		},
	}

	provider, err := dns.CreateProvider(cfg, logger)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "hetzner", provider.Name())
}

func TestCreateProvider_Hetzner_EmptyToken(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.DNSConfig{
		Provider: "hetzner",
		Hetzner: &config.HetznerConfig{
			APIToken: "",
			ZoneID:   "test-zone",
		},
	}

	// hetzner provider constructor returns nil for empty token, which causes error
	provider, err := dns.CreateProvider(cfg, logger)
	require.Error(t, err)
	assert.Nil(t, provider)
}
