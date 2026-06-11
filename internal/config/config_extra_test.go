package config_test

// config_extra_test.go adds table-driven tests for the methods that were at
// 0 % coverage: Route53Config.Validate, HetznerConfig.Validate,
// HetznerConfig.String, plus the missing branches in Config.Validate and
// DNSConfig.Validate.

import (
	"strings"
	"testing"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Route53Config.Validate
// ---------------------------------------------------------------------------

func TestRoute53Config_Validate(t *testing.T) {
	valid := &config.Route53Config{
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region:          "us-east-1",
		HostedZoneID:    "Z1234567890",
	}

	tests := []struct {
		name        string
		mutate      func(*config.Route53Config)
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid",
			mutate:  func(*config.Route53Config) {},
			wantErr: false,
		},
		{
			name:        "missing access_key_id",
			mutate:      func(c *config.Route53Config) { c.AccessKeyID = "" },
			wantErr:     true,
			errContains: "access_key_id is required",
		},
		{
			name:        "missing secret_access_key",
			mutate:      func(c *config.Route53Config) { c.SecretAccessKey = "" },
			wantErr:     true,
			errContains: "secret_access_key is required",
		},
		{
			name:        "missing region",
			mutate:      func(c *config.Route53Config) { c.Region = "" },
			wantErr:     true,
			errContains: "region is required",
		},
		{
			name:        "missing hosted_zone_id",
			mutate:      func(c *config.Route53Config) { c.HostedZoneID = "" },
			wantErr:     true,
			errContains: "hosted_zone_id is required",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Shallow copy so each test case is independent.
			cfg := *valid
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HetznerConfig.Validate
// ---------------------------------------------------------------------------

func TestHetznerConfig_Validate(t *testing.T) {
	valid := &config.HetznerConfig{
		APIToken: "hetzner-api-token",
		ZoneID:   "zone-123",
	}

	tests := []struct {
		name        string
		mutate      func(*config.HetznerConfig)
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid",
			mutate:  func(*config.HetznerConfig) {},
			wantErr: false,
		},
		{
			name:        "missing api_token",
			mutate:      func(c *config.HetznerConfig) { c.APIToken = "" },
			wantErr:     true,
			errContains: "api_token is required",
		},
		{
			name:        "missing zone_id",
			mutate:      func(c *config.HetznerConfig) { c.ZoneID = "" },
			wantErr:     true,
			errContains: "zone_id is required",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := *valid
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HetznerConfig.String  (was 0 %)
// ---------------------------------------------------------------------------

func TestHetznerConfig_String(t *testing.T) {
	cfg := &config.HetznerConfig{
		APIToken: "super-secret-hetzner-token",
		ZoneID:   "zone-abc-123",
	}

	result := cfg.String()

	assert.Contains(t, result, "[REDACTED]", "api_token must be redacted")
	assert.Contains(t, result, "zone-abc-123", "zone_id must appear in output")
	assert.False(t,
		strings.Contains(result, "super-secret-hetzner-token"),
		"raw api_token must not appear in output",
	)
}

// ---------------------------------------------------------------------------
// Config.Validate — missing branches
// ---------------------------------------------------------------------------

func TestConfig_Validate_MissingBranches(t *testing.T) {
	base := func() *config.Config {
		return &config.Config{
			PollInterval:         30 * time.Second,
			CheckEndpoints:       []string{"https://ifconfig.io/ip"},
			PrimaryIP:            "203.0.113.10",
			SecondaryIP:          "198.51.100.77",
			StateFile:            "/tmp/state.json",
			StateFailureStrategy: "continue_with_warning",
			FailoverRetries:      3,
			DNS: []config.DNSConfig{
				{
					Name:     "example.com",
					Type:     "A",
					Provider: "cloudflare",
					TTL:      300,
					Cloudflare: &config.CloudflareConfig{
						APIToken: "tok",
						ZoneID:   "zone",
					},
				},
			},
		}
	}

	tests := []struct {
		name        string
		mutate      func(*config.Config)
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid baseline",
			mutate:  func(*config.Config) {},
			wantErr: false,
		},
		{
			name:        "negative failover_retries",
			mutate:      func(c *config.Config) { c.FailoverRetries = -1 },
			wantErr:     true,
			errContains: "failover_retries must be non-negative",
		},
		{
			name:        "invalid state_failure_strategy",
			mutate:      func(c *config.Config) { c.StateFailureStrategy = "invalid_strategy" },
			wantErr:     true,
			errContains: "state_failure_strategy must be one of",
		},
		{
			name: "dns record validation propagated",
			mutate: func(c *config.Config) {
				// A DNS entry that fails its own Validate (missing Name).
				c.DNS = []config.DNSConfig{{}}
			},
			wantErr:     true,
			errContains: "DNS record 0 validation failed",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(cfg)
			err := cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DNSConfig.Validate — missing provider branches (cpanel, route53, hetzner)
// ---------------------------------------------------------------------------

func TestDNSConfig_Validate_AllProviders(t *testing.T) {
	tests := []struct {
		name        string
		dns         config.DNSConfig
		wantErr     bool
		errContains string
	}{
		// ---- cloudflare (already tested separately; here for completeness) ----
		{
			name: "cloudflare valid",
			dns: config.DNSConfig{
				Name: "cf.example.com", Type: "A", Provider: "cloudflare", TTL: 300,
				Cloudflare: &config.CloudflareConfig{APIToken: "tok", ZoneID: "z"},
			},
			wantErr: false,
		},
		{
			name:        "cloudflare invalid inner config",
			wantErr:     true,
			errContains: "cloudflare config validation failed",
			dns: config.DNSConfig{
				Name: "cf.example.com", Type: "A", Provider: "cloudflare", TTL: 300,
				// ZoneID missing → Cloudflare.Validate() returns error
				Cloudflare: &config.CloudflareConfig{APIToken: "tok", ZoneID: ""},
			},
		},
		// ---- cpanel ----
		{
			name: "cpanel valid",
			dns: config.DNSConfig{
				Name: "cp.example.com", Type: "A", Provider: "cpanel", TTL: 300,
				CPanel: &config.CPanelConfig{
					BaseURL: "https://cpanel.example.com", Username: "u",
					APIToken: "t", Zone: "example.com",
				},
			},
			wantErr: false,
		},
		{
			name:        "cpanel nil config",
			wantErr:     true,
			errContains: "cpanel configuration is required",
			dns: config.DNSConfig{
				Name: "cp.example.com", Type: "A", Provider: "cpanel", TTL: 300,
			},
		},
		{
			name:        "cpanel invalid inner config",
			wantErr:     true,
			errContains: "cpanel config validation failed",
			dns: config.DNSConfig{
				Name: "cp.example.com", Type: "A", Provider: "cpanel", TTL: 300,
				CPanel: &config.CPanelConfig{
					BaseURL: "https://cpanel.example.com", Username: "u",
					APIToken: "t", Zone: "",
				},
			},
		},
		// ---- route53 ----
		{
			name: "route53 valid",
			dns: config.DNSConfig{
				Name: "r53.example.com", Type: "A", Provider: "route53", TTL: 300,
				Route53: &config.Route53Config{
					AccessKeyID: "K", SecretAccessKey: "S",
					Region: "us-east-1", HostedZoneID: "ZID",
				},
			},
			wantErr: false,
		},
		{
			name:        "route53 nil config",
			wantErr:     true,
			errContains: "route53 configuration is required",
			dns: config.DNSConfig{
				Name: "r53.example.com", Type: "A", Provider: "route53", TTL: 300,
			},
		},
		{
			name:        "route53 invalid inner config",
			wantErr:     true,
			errContains: "route53 config validation failed",
			dns: config.DNSConfig{
				Name: "r53.example.com", Type: "A", Provider: "route53", TTL: 300,
				Route53: &config.Route53Config{
					AccessKeyID: "K", SecretAccessKey: "S",
					Region: "us-east-1", HostedZoneID: "",
				},
			},
		},
		// ---- hetzner ----
		{
			name: "hetzner valid",
			dns: config.DNSConfig{
				Name: "hz.example.com", Type: "A", Provider: "hetzner", TTL: 300,
				Hetzner: &config.HetznerConfig{APIToken: "t", ZoneID: "z"},
			},
			wantErr: false,
		},
		{
			name:        "hetzner nil config",
			wantErr:     true,
			errContains: "hetzner configuration is required",
			dns: config.DNSConfig{
				Name: "hz.example.com", Type: "A", Provider: "hetzner", TTL: 300,
			},
		},
		{
			name:        "hetzner invalid inner config",
			wantErr:     true,
			errContains: "hetzner config validation failed",
			dns: config.DNSConfig{
				Name: "hz.example.com", Type: "A", Provider: "hetzner", TTL: 300,
				Hetzner: &config.HetznerConfig{APIToken: "t", ZoneID: ""},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.dns.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
