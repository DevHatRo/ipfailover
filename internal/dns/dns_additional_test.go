package dns_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/internal/dns"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- Constructor edge cases ---

func TestNewCloudflareProvider_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	p := dns.NewCloudflareProvider(nil, logger)
	assert.Nil(t, p)
}

func TestNewCloudflareProvider_NilLogger(t *testing.T) {
	cfg := &config.CloudflareConfig{APIToken: "tok", ZoneID: "z"}
	p := dns.NewCloudflareProvider(cfg, nil)
	assert.NotNil(t, p)
}

func TestNewCloudflareProviderWithClient_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	p := dns.NewCloudflareProviderWithClient(nil, nil, logger)
	assert.Nil(t, p)
}

func TestNewCloudflareProviderWithClient_NilClient(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.CloudflareConfig{APIToken: "tok", ZoneID: "z"}
	p := dns.NewCloudflareProviderWithClient(cfg, nil, logger)
	assert.NotNil(t, p)
}

func TestNewCPanelProvider_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	_, err := dns.NewCPanelProvider(nil, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config must not be nil")
}

func TestNewCPanelProvider_NilLogger(t *testing.T) {
	cfg := &config.CPanelConfig{
		BaseURL:  "https://cp.example.com",
		Username: "user",
		APIToken: "tok",
		Zone:     "example.com",
	}
	_, err := dns.NewCPanelProvider(cfg, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "logger must not be nil")
}

func TestNewHetznerProvider_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	p := dns.NewHetznerProvider(nil, logger)
	assert.Nil(t, p)
}

func TestNewHetznerProvider_EmptyToken(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.HetznerConfig{APIToken: "  ", ZoneID: "z"}
	p := dns.NewHetznerProvider(cfg, logger)
	assert.Nil(t, p)
}

func TestNewHetznerProviderWithClient_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	p := dns.NewHetznerProviderWithClient(nil, nil, logger)
	assert.Nil(t, p)
}

func TestNewHetznerProviderWithClient_NilClient_EmptyToken(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.HetznerConfig{APIToken: "", ZoneID: "z"}
	p := dns.NewHetznerProviderWithClient(cfg, nil, logger)
	assert.Nil(t, p)
}

// --- Config validation edge cases ---

func TestCloudflareConfig_Validate_Missing(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.CloudflareConfig
		want string
	}{
		{"missing token", config.CloudflareConfig{ZoneID: "z"}, "api_token is required"},
		{"missing zone", config.CloudflareConfig{APIToken: "t"}, "zone_id is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestCPanelConfig_Validate_Missing(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.CPanelConfig
		want string
	}{
		{"missing base_url", config.CPanelConfig{Username: "u", APIToken: "t", Zone: "z"}, "base_url is required"},
		{"missing username", config.CPanelConfig{BaseURL: "http://x", APIToken: "t", Zone: "z"}, "username is required"},
		{"missing api_token", config.CPanelConfig{BaseURL: "http://x", Username: "u", Zone: "z"}, "api_token is required"},
		{"missing zone", config.CPanelConfig{BaseURL: "http://x", Username: "u", APIToken: "t"}, "zone is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestRoute53Config_Validate_Missing(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Route53Config
		want string
	}{
		{"missing access_key", config.Route53Config{SecretAccessKey: "s", Region: "r", HostedZoneID: "h"}, "access_key_id is required"},
		{"missing secret", config.Route53Config{AccessKeyID: "a", Region: "r", HostedZoneID: "h"}, "secret_access_key is required"},
		{"missing region", config.Route53Config{AccessKeyID: "a", SecretAccessKey: "s", HostedZoneID: "h"}, "region is required"},
		{"missing zone", config.Route53Config{AccessKeyID: "a", SecretAccessKey: "s", Region: "r"}, "hosted_zone_id is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// --- Config String() redaction ---

func TestConfigString_Redaction(t *testing.T) {
	t.Run("CloudflareConfig redacts token", func(t *testing.T) {
		cfg := &config.CloudflareConfig{APIToken: "secret123", ZoneID: "z1", Proxied: true}
		s := cfg.String()
		assert.Contains(t, s, "[REDACTED]")
		assert.NotContains(t, s, "secret123")
		assert.Contains(t, s, "z1")
	})

	t.Run("CPanelConfig redacts token", func(t *testing.T) {
		cfg := &config.CPanelConfig{BaseURL: "http://x", Username: "user", APIToken: "secret", Zone: "z"}
		s := cfg.String()
		assert.Contains(t, s, "[REDACTED]")
		assert.NotContains(t, s, "secret")
	})

	t.Run("Route53Config redacts credentials", func(t *testing.T) {
		cfg := &config.Route53Config{AccessKeyID: "ak", SecretAccessKey: "sk", Region: "us-east-1", HostedZoneID: "hz"}
		s := cfg.String()
		assert.Contains(t, s, "[REDACTED]")
		assert.NotContains(t, s, "ak")
		assert.NotContains(t, s, "sk")
	})

	t.Run("HetznerConfig redacts token", func(t *testing.T) {
		cfg := &config.HetznerConfig{APIToken: "secret", ZoneID: "z1"}
		s := cfg.String()
		assert.Contains(t, s, "[REDACTED]")
		assert.NotContains(t, s, "secret")
	})
}

// --- cPanel full CRUD with httptest ---

func newCPanelTestServer(t *testing.T, records []cpanelRecord) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.Contains(r.URL.Path, "get_dns_records") || strings.Contains(r.URL.Path, "DnsLookup/get_dns_records"):
			resp := cpanelResponse{
				Result: cpanelResult{
					Data: records,
					Meta: cpanelMeta{Result: 1},
				},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("failed to encode: %v", err)
			}
		case strings.Contains(r.URL.Path, "update_dns_record"):
			resp := cpanelResponse{
				Result: cpanelResult{Meta: cpanelMeta{Result: 1}},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("failed to encode: %v", err)
			}
		case strings.Contains(r.URL.Path, "add_dns_record"):
			resp := cpanelResponse{
				Result: cpanelResult{Meta: cpanelMeta{Result: 1}},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("failed to encode: %v", err)
			}
		case strings.Contains(r.URL.Path, "delete_dns_record"):
			resp := cpanelResponse{
				Result: cpanelResult{Meta: cpanelMeta{Result: 1}},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("failed to encode: %v", err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

type cpanelRecord struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	Record string `json:"record"`
	Data   string `json:"data"`
	TTL    int    `json:"ttl"`
	Serial int    `json:"serial"`
	Line   int    `json:"line"`
}

type cpanelMeta struct {
	Result int `json:"result"`
}

type cpanelResult struct {
	Data []cpanelRecord `json:"data"`
	Meta cpanelMeta     `json:"meta"`
}

type cpanelResponse struct {
	Result cpanelResult `json:"result"`
}

func TestCPanelProvider_FullCRUD(t *testing.T) {
	logger := zap.NewNop()

	t.Run("GetRecord returns found record", func(t *testing.T) {
		server := newCPanelTestServer(t, []cpanelRecord{
			{ID: "1", Type: "A", Name: "test.example.com", Data: "1.2.3.4", TTL: 300, Line: 1},
		})
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		rec, err := provider.GetRecord(t.Context(), "test.example.com", "A")
		require.NoError(t, err)
		require.NotNil(t, rec)
		assert.Equal(t, "test.example.com", rec.Name)
		assert.Equal(t, "1.2.3.4", rec.Value)
	})

	t.Run("GetRecord returns nil for not found", func(t *testing.T) {
		server := newCPanelTestServer(t, []cpanelRecord{})
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		rec, err := provider.GetRecord(t.Context(), "missing.example.com", "A")
		require.NoError(t, err)
		assert.Nil(t, rec)
	})

	t.Run("UpdateRecord creates new record", func(t *testing.T) {
		server := newCPanelTestServer(t, []cpanelRecord{})
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		record := interfaces.DNSRecord{Name: "new.example.com", Type: "A", Value: "5.6.7.8", TTL: 300}
		err = provider.UpdateRecord(t.Context(), record)
		require.NoError(t, err)
	})

	t.Run("UpdateRecord updates existing record", func(t *testing.T) {
		server := newCPanelTestServer(t, []cpanelRecord{
			{ID: "1", Type: "A", Name: "test.example.com", Data: "1.2.3.4", TTL: 300, Line: 5},
		})
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		record := interfaces.DNSRecord{Name: "test.example.com", Type: "A", Value: "9.8.7.6", TTL: 300}
		err = provider.UpdateRecord(t.Context(), record)
		require.NoError(t, err)
	})

	t.Run("DeleteRecord - existing", func(t *testing.T) {
		server := newCPanelTestServer(t, []cpanelRecord{
			{ID: "1", Type: "A", Name: "test.example.com", Data: "1.2.3.4", TTL: 300, Line: 1},
		})
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		err = provider.DeleteRecord(t.Context(), "test.example.com", "A")
		require.NoError(t, err)
	})

	t.Run("DeleteRecord - not found is ok", func(t *testing.T) {
		server := newCPanelTestServer(t, []cpanelRecord{})
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		err = provider.DeleteRecord(t.Context(), "missing.example.com", "A")
		require.NoError(t, err)
	})

	t.Run("Validate succeeds", func(t *testing.T) {
		server := newCPanelTestServer(t, []cpanelRecord{})
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		err = provider.Validate(t.Context())
		require.NoError(t, err)
	})
}

func TestCPanelProvider_HTTPErrors(t *testing.T) {
	logger := zap.NewNop()

	t.Run("listRecords HTTP 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		_, err = provider.GetRecord(t.Context(), "test.example.com", "A")
		require.Error(t, err)
	})

	t.Run("listRecords API error code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := cpanelResponse{Result: cpanelResult{Meta: cpanelMeta{Result: 0}}}
			if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
				t.Errorf("encode error: %v", encErr)
			}
		}))
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		_, err = provider.GetRecord(t.Context(), "test.example.com", "A")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "result code 0")
	})

	t.Run("update HTTP 500", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			if callCount == 1 {
				// First call is listRecords (from findRecord) - return existing record
				resp := cpanelResponse{
					Result: cpanelResult{
						Data: []cpanelRecord{{ID: "1", Type: "A", Name: "test.example.com", Data: "old", TTL: 300, Line: 1}},
						Meta: cpanelMeta{Result: 1},
					},
				}
				if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
					t.Errorf("encode error: %v", encErr)
				}
				return
			}
			// Second call is update - return error
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cfg := &config.CPanelConfig{BaseURL: server.URL, Username: "user", APIToken: "tok", Zone: "example.com"}
		provider, err := dns.NewCPanelProvider(cfg, logger)
		require.NoError(t, err)

		rec := interfaces.DNSRecord{Name: "test.example.com", Type: "A", Value: "1.2.3.4", TTL: 300}
		err = provider.UpdateRecord(t.Context(), rec)
		require.Error(t, err)
	})
}

// --- Route53 edge cases ---

func TestRoute53Provider_UpdateRecord_EmptyType(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Route53Config{
		AccessKeyID: "ak", SecretAccessKey: "sk", Region: "us-east-1", HostedZoneID: "hz",
	}
	provider, err := dns.NewRoute53Provider(cfg, logger)
	require.NoError(t, err)

	record := interfaces.DNSRecord{Name: "test.example.com", Type: "", Value: "1.2.3.4", TTL: 300}
	err = provider.UpdateRecord(t.Context(), record)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty record type")
}

func TestRoute53Provider_GetRecord_EmptyType(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Route53Config{
		AccessKeyID: "ak", SecretAccessKey: "sk", Region: "us-east-1", HostedZoneID: "hz",
	}
	provider, err := dns.NewRoute53Provider(cfg, logger)
	require.NoError(t, err)

	rec, err := provider.GetRecord(t.Context(), "test.example.com", "")
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestRoute53Provider_DeleteRecord_EmptyType(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Route53Config{
		AccessKeyID: "ak", SecretAccessKey: "sk", Region: "us-east-1", HostedZoneID: "hz",
	}
	provider, err := dns.NewRoute53Provider(cfg, logger)
	require.NoError(t, err)

	err = provider.DeleteRecord(t.Context(), "test.example.com", "")
	require.Error(t, err)
}

// --- DNSConfig validation ---

func TestDNSConfig_Validate(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.DNSConfig
		want string
	}{
		{"missing name", config.DNSConfig{Type: "A", Provider: "cloudflare", TTL: 300, Cloudflare: &config.CloudflareConfig{APIToken: "t", ZoneID: "z"}}, "name is required"},
		{"missing type", config.DNSConfig{Name: "n", Provider: "cloudflare", TTL: 300, Cloudflare: &config.CloudflareConfig{APIToken: "t", ZoneID: "z"}}, "type is required"},
		{"missing provider", config.DNSConfig{Name: "n", Type: "A", TTL: 300}, "provider is required"},
		{"zero TTL", config.DNSConfig{Name: "n", Type: "A", Provider: "cloudflare", TTL: 0, Cloudflare: &config.CloudflareConfig{APIToken: "t", ZoneID: "z"}}, "TTL must be positive"},
		{"unsupported provider", config.DNSConfig{Name: "n", Type: "A", Provider: "unknown", TTL: 300}, "unsupported provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
