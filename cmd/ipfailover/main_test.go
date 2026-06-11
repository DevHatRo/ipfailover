package main

import (
	"strings"
	"testing"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// getVersion
// ---------------------------------------------------------------------------

func TestGetVersion(t *testing.T) {
	// Restore original values after the test.
	origVersion := Version
	origBuildTime := BuildTime
	t.Cleanup(func() {
		Version = origVersion
		BuildTime = origBuildTime
	})

	Version = "1.2.3"
	BuildTime = "2026-01-01T00:00:00Z"

	v := getVersion()
	assert.Contains(t, v, "1.2.3", "version string must contain Version")
	assert.Contains(t, v, "2026-01-01T00:00:00Z", "version string must contain BuildTime")
}

func TestGetVersion_Defaults(t *testing.T) {
	// The package-level defaults are "dev" / "unknown".
	v := getVersion()
	assert.NotEmpty(t, v)
	assert.Contains(t, v, Version)
	assert.Contains(t, v, BuildTime)
}

// ---------------------------------------------------------------------------
// setupLogging
// ---------------------------------------------------------------------------

func TestSetupLogging(t *testing.T) {
	tests := []struct {
		level string
	}{
		{"debug"},
		{"info"},
		{"warn"},
		{"error"},
		{"unknown"},
		{""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run("level="+tc.level, func(t *testing.T) {
			logger, err := setupLogging(tc.level)
			require.NoError(t, err, "setupLogging(%q) must not return an error", tc.level)
			require.NotNil(t, logger, "setupLogging(%q) must return a non-nil logger", tc.level)
		})
	}
}

// ---------------------------------------------------------------------------
// buildApplication
// ---------------------------------------------------------------------------

// minimalConfig returns a *config.Config that passes Validate() with no DNS
// providers that need real network credentials. Because Validate() requires at
// least one DNS entry we add a cloudflare entry.
func minimalValidConfig() *config.Config {
	return &config.Config{
		PollInterval:         30 * time.Second,
		CheckEndpoints:       []string{"https://ifconfig.io/ip"},
		PrimaryIP:            "203.0.113.10",
		SecondaryIP:          "198.51.100.77",
		StateFile:            "/tmp/ipfailover-test-state.json",
		FailoverRetries:      3,
		ReachabilityPort:     "80",
		ReachabilityTimeout:  3 * time.Second,
		MetricsAddr:          ":0",
		LogLevel:             "info",
		StateFailureStrategy: "continue_with_warning",
		DNS: []config.DNSConfig{
			{
				Name:     "example.com",
				Type:     "A",
				Provider: "cloudflare",
				TTL:      300,
				Cloudflare: &config.CloudflareConfig{
					APIToken: "test-token",
					ZoneID:   "test-zone",
					Proxied:  false,
				},
			},
		},
	}
}

func TestBuildApplication_Success(t *testing.T) {
	cfg := minimalValidConfig()
	logger := zap.NewNop()

	app, err := buildApplication(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, app)
}

func TestBuildApplication_WithWebhook(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.WebhookURL = "https://example.com/webhook"
	logger := zap.NewNop()

	app, err := buildApplication(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, app)
}

func TestBuildApplication_WithSlackWebhook(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.SlackWebhookURL = "https://hooks.slack.com/services/T00/B00/xxx"
	cfg.SlackChannel = "#alerts"
	logger := zap.NewNop()

	app, err := buildApplication(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, app)
}

func TestBuildApplication_UnknownProvider_ReturnsError(t *testing.T) {
	cfg := minimalValidConfig()
	// Replace the DNS entry with an unknown provider. We bypass Validate() by
	// constructing the struct directly — buildApplication does not re-validate.
	cfg.DNS = []config.DNSConfig{
		{
			Name:     "example.com",
			Type:     "A",
			Provider: "nonexistent-provider",
			TTL:      300,
		},
	}
	logger := zap.NewNop()

	_, err := buildApplication(cfg, logger)
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "unsupported") || strings.Contains(err.Error(), "failed to create DNS provider"),
		"expected an unsupported/creation error, got: %v", err,
	)
}

func TestBuildApplication_NoDNSEntries(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.DNS = nil
	logger := zap.NewNop()

	// No DNS entries means no providers are iterated; buildApplication should
	// succeed (config validation is the caller's responsibility).
	app, err := buildApplication(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, app)
}

// ---------------------------------------------------------------------------
// createDNSProvider
// ---------------------------------------------------------------------------

func TestCreateDNSProvider_InvalidProvider_ReturnsError(t *testing.T) {
	dnsConfig := config.DNSConfig{
		Name:     "example.com",
		Type:     "A",
		Provider: "definitely-not-a-real-provider",
		TTL:      300,
	}
	logger := zap.NewNop()

	_, err := createDNSProvider(dnsConfig, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestCreateDNSProvider_KnownProvider_Cloudflare(t *testing.T) {
	dnsConfig := config.DNSConfig{
		Name:     "example.com",
		Type:     "A",
		Provider: "cloudflare",
		TTL:      300,
		Cloudflare: &config.CloudflareConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		},
	}
	logger := zap.NewNop()

	provider, err := createDNSProvider(dnsConfig, logger)
	require.NoError(t, err)
	require.NotNil(t, provider)
}
