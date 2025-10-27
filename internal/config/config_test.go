package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.yaml")

		configContent := `
poll_interval: "30s"
check_endpoints:
  - "https://ifconfig.io/ip"
  - "https://api.ipify.org"
primary_ip: "203.0.113.10"
secondary_ip: "198.51.100.77"
state_file: "/tmp/state.json"
metrics_addr: ":8080"
log_level: "info"
dns:
  - name: "example.com"
    type: "A"
    provider: "cloudflare"
    ttl: 300
    cloudflare:
      api_token: "test-token"
      zone_id: "test-zone"
      proxied: false
`

		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		cfg, err := config.LoadConfig(configFile)

		assert.NoError(t, err)
		assert.Equal(t, 30*time.Second, cfg.PollInterval)
		assert.Equal(t, []string{"https://ifconfig.io/ip", "https://api.ipify.org"}, cfg.CheckEndpoints)
		assert.Equal(t, "203.0.113.10", cfg.PrimaryIP)
		assert.Equal(t, "198.51.100.77", cfg.SecondaryIP)
		assert.Equal(t, "/tmp/state.json", cfg.StateFile)
		assert.Equal(t, ":8080", cfg.MetricsAddr)
		assert.Equal(t, "info", cfg.LogLevel)
		assert.Len(t, cfg.DNS, 1)
		assert.Equal(t, "example.com", cfg.DNS[0].Name)
		assert.Equal(t, "A", cfg.DNS[0].Type)
		assert.Equal(t, "cloudflare", cfg.DNS[0].Provider)
		assert.Equal(t, 300, cfg.DNS[0].TTL)
		assert.NotNil(t, cfg.DNS[0].Cloudflare)
		assert.Equal(t, "test-token", cfg.DNS[0].Cloudflare.APIToken)
		assert.Equal(t, "test-zone", cfg.DNS[0].Cloudflare.ZoneID)
		assert.False(t, cfg.DNS[0].Cloudflare.Proxied)
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := config.LoadConfig("/nonexistent/config.yaml")
		assert.Error(t, err)
	})

	t.Run("invalid YAML", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.yaml")

		configContent := `invalid: yaml: content: [`

		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		_, err := config.LoadConfig(configFile)
		assert.Error(t, err)
	})
}

func TestConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &config.Config{
			PollInterval:   30 * time.Second,
			CheckEndpoints: []string{"https://ifconfig.io/ip"},
			PrimaryIP:      "203.0.113.10",
			SecondaryIP:    "198.51.100.77",
			StateFile:      "/tmp/state.json",
			DNS: []config.DNSConfig{
				{
					Name:     "example.com",
					Type:     "A",
					Provider: "cloudflare",
					TTL:      300,
					Cloudflare: &config.CloudflareConfig{
						APIToken: "test-token",
						ZoneID:   "test-zone",
					},
				},
			},
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("invalid poll interval", func(t *testing.T) {
		cfg := &config.Config{
			PollInterval: -1,
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "poll_interval must be positive")
	})

	t.Run("empty check endpoints", func(t *testing.T) {
		cfg := &config.Config{
			PollInterval:   30 * time.Second,
			CheckEndpoints: []string{},
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one check_endpoint must be specified")
	})

	t.Run("empty primary IP", func(t *testing.T) {
		cfg := &config.Config{
			PollInterval:   30 * time.Second,
			CheckEndpoints: []string{"https://ifconfig.io/ip"},
			PrimaryIP:      "",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "primary_ip must be specified")
	})

	t.Run("empty secondary IP", func(t *testing.T) {
		cfg := &config.Config{
			PollInterval:   30 * time.Second,
			CheckEndpoints: []string{"https://ifconfig.io/ip"},
			PrimaryIP:      "203.0.113.10",
			SecondaryIP:    "",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "secondary_ip must be specified")
	})

	t.Run("empty state file", func(t *testing.T) {
		cfg := &config.Config{
			PollInterval:   30 * time.Second,
			CheckEndpoints: []string{"https://ifconfig.io/ip"},
			PrimaryIP:      "203.0.113.10",
			SecondaryIP:    "198.51.100.77",
			StateFile:      "",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state_file must be specified")
	})

	t.Run("empty DNS records", func(t *testing.T) {
		cfg := &config.Config{
			PollInterval:   30 * time.Second,
			CheckEndpoints: []string{"https://ifconfig.io/ip"},
			PrimaryIP:      "203.0.113.10",
			SecondaryIP:    "198.51.100.77",
			StateFile:      "/tmp/state.json",
			DNS:            []config.DNSConfig{},
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one DNS record must be configured")
	})
}

func TestDNSConfig_Validate(t *testing.T) {
	t.Run("valid cloudflare config", func(t *testing.T) {
		dns := config.DNSConfig{
			Name:     "example.com",
			Type:     "A",
			Provider: "cloudflare",
			TTL:      300,
			Cloudflare: &config.CloudflareConfig{
				APIToken: "test-token",
				ZoneID:   "test-zone",
			},
		}

		err := dns.Validate()
		assert.NoError(t, err)
	})

	t.Run("empty name", func(t *testing.T) {
		dns := config.DNSConfig{
			Name: "",
		}

		err := dns.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("empty type", func(t *testing.T) {
		dns := config.DNSConfig{
			Name: "example.com",
			Type: "",
		}

		err := dns.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type is required")
	})

	t.Run("empty provider", func(t *testing.T) {
		dns := config.DNSConfig{
			Name:     "example.com",
			Type:     "A",
			Provider: "",
		}

		err := dns.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "provider is required")
	})

	t.Run("invalid TTL", func(t *testing.T) {
		dns := config.DNSConfig{
			Name:     "example.com",
			Type:     "A",
			Provider: "cloudflare",
			TTL:      0,
		}

		err := dns.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TTL must be positive")
	})

	t.Run("unsupported provider", func(t *testing.T) {
		dns := config.DNSConfig{
			Name:     "example.com",
			Type:     "A",
			Provider: "unsupported",
			TTL:      300,
		}

		err := dns.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported provider")
	})

	t.Run("cloudflare provider without config", func(t *testing.T) {
		dns := config.DNSConfig{
			Name:     "example.com",
			Type:     "A",
			Provider: "cloudflare",
			TTL:      300,
		}

		err := dns.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cloudflare configuration is required")
	})
}

func TestCloudflareConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &config.CloudflareConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("empty API token", func(t *testing.T) {
		cfg := &config.CloudflareConfig{
			APIToken: "",
			ZoneID:   "test-zone",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api_token is required")
	})

	t.Run("empty zone ID", func(t *testing.T) {
		cfg := &config.CloudflareConfig{
			APIToken: "test-token",
			ZoneID:   "",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "zone_id is required")
	})
}

func TestCPanelConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &config.CPanelConfig{
			BaseURL:  "https://cpanel.example.com",
			Username: "testuser",
			APIToken: "test-token",
			Zone:     "example.com",
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("empty base URL", func(t *testing.T) {
		cfg := &config.CPanelConfig{
			BaseURL: "",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "base_url is required")
	})

	t.Run("empty username", func(t *testing.T) {
		cfg := &config.CPanelConfig{
			BaseURL:  "https://cpanel.example.com",
			Username: "",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})

	t.Run("empty API token", func(t *testing.T) {
		cfg := &config.CPanelConfig{
			BaseURL:  "https://cpanel.example.com",
			Username: "testuser",
			APIToken: "",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api_token is required")
	})

	t.Run("empty zone", func(t *testing.T) {
		cfg := &config.CPanelConfig{
			BaseURL:  "https://cpanel.example.com",
			Username: "testuser",
			APIToken: "test-token",
			Zone:     "",
		}

		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "zone is required")
	})
}
