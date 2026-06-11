package dns_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudflare/cloudflare-go/v2"
	"github.com/cloudflare/cloudflare-go/v2/option"
	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/internal/dns"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// cfRecord is the shape returned by the Cloudflare API for a single DNS record.
type cfRecord struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Content interface{} `json:"content"`
	TTL     int         `json:"ttl"`
	Proxied bool        `json:"proxied"`
}

// cfListResponse is the shape returned by GET /zones/:id/dns_records.
type cfListResponse struct {
	Result  []cfRecord `json:"result"`
	Success bool       `json:"success"`
}

// cfSingleResponse is the shape returned by POST/PUT on a DNS record.
type cfSingleResponse struct {
	Result  cfRecord `json:"result"`
	Success bool     `json:"success"`
}

// cfDeleteResponse is the shape returned by DELETE on a DNS record.
type cfDeleteResponse struct {
	Result  map[string]string `json:"result"`
	Success bool              `json:"success"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(fmt.Sprintf("writeJSON encode: %v", err))
	}
}

// newCloudflareTestProvider creates a CloudflareProvider wired to a fake httptest server.
func newCloudflareTestProvider(t *testing.T, mux *http.ServeMux) *dns.CloudflareProvider {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	cfg := &config.CloudflareConfig{
		APIToken: "test-token",
		ZoneID:   "zone-abc",
	}
	logger := zap.NewNop()

	client := cloudflare.NewClient(
		option.WithAPIToken("test-token"),
		option.WithBaseURL(server.URL+"/"),
	)
	return dns.NewCloudflareProviderWithClient(cfg, client, logger)
}

// --- createRecordParam coverage ---

func TestCloudflareProvider_CreateRecordParam_AllTypes(t *testing.T) {
	// We exercise createRecordParam indirectly by calling UpdateRecord (create path).
	// Each type triggers a POST after an empty list response.
	tests := []struct {
		rtype    string
		value    string
		metadata map[string]string
	}{
		{"A", "1.2.3.4", nil},
		{"AAAA", "::1", nil},
		{"CNAME", "alias.example.com", nil},
		{"TXT", "v=spf1 ~all", nil},
		{"MX", "mail.example.com", map[string]string{"priority": "10"}},
		{"MX", "mail.example.com", map[string]string{"priority": "badnum"}}, // fallback priority
		{"MX", "mail.example.com", nil},                                     // no priority metadata
		{"NS", "ns1.example.com", nil},
		{"PTR", "host.example.com", nil},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.rtype+"_"+tt.value, func(t *testing.T) {
			mux := http.NewServeMux()

			// List → empty (triggers create path)
			mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					writeJSON(w, http.StatusOK, cfListResponse{Result: nil, Success: true})
					return
				}
				if r.Method == http.MethodPost {
					resp := cfSingleResponse{
						Result: cfRecord{
							ID:      "new-id",
							Name:    "test.example.com",
							Type:    tt.rtype,
							Content: tt.value,
							TTL:     300,
						},
						Success: true,
					}
					writeJSON(w, http.StatusOK, resp)
					return
				}
				w.WriteHeader(http.StatusMethodNotAllowed)
			})

			provider := newCloudflareTestProvider(t, mux)
			record := interfaces.DNSRecord{
				Name:     "test.example.com",
				Type:     tt.rtype,
				Value:    tt.value,
				TTL:      300,
				Provider: "cloudflare",
				Metadata: tt.metadata,
			}
			err := provider.UpdateRecord(context.Background(), record)
			require.NoError(t, err)
		})
	}
}

func TestCloudflareProvider_CreateRecordParam_UnsupportedType(t *testing.T) {
	// To trigger createRecordParam with an unsupported type we need a record
	// to exist first so the update path (which calls createRecordParam) is used.
	mux := http.NewServeMux()

	// List returns existing record with id "r1"
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, cfListResponse{
				Result:  []cfRecord{{ID: "r1", Name: "test.example.com", Type: "SRV", Content: "something", TTL: 300}},
				Success: true,
			})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	provider := newCloudflareTestProvider(t, mux)
	record := interfaces.DNSRecord{
		Name:     "test.example.com",
		Type:     "SRV", // unsupported in createRecordParam
		Value:    "something",
		TTL:      300,
		Provider: "cloudflare",
	}
	err := provider.UpdateRecord(context.Background(), record)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported DNS record type")
}

// --- UpdateRecord ---

func TestCloudflareProvider_UpdateRecord_CreatePath(t *testing.T) {
	mux := http.NewServeMux()

	created := false
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, cfListResponse{Success: true})
			return
		}
		if r.Method == http.MethodPost {
			created = true
			writeJSON(w, http.StatusOK, cfSingleResponse{
				Result:  cfRecord{ID: "new-id", Name: "sub.example.com", Type: "A", Content: "5.6.7.8", TTL: 60},
				Success: true,
			})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name:     "sub.example.com",
		Type:     "A",
		Value:    "5.6.7.8",
		TTL:      60,
		Provider: "cloudflare",
	})
	require.NoError(t, err)
	assert.True(t, created, "expected POST to /dns_records")
}

func TestCloudflareProvider_UpdateRecord_UpdatePath(t *testing.T) {
	mux := http.NewServeMux()

	updated := false
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, cfListResponse{
				Result:  []cfRecord{{ID: "existing-id", Name: "sub.example.com", Type: "A", Content: "1.2.3.4", TTL: 300}},
				Success: true,
			})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/zones/zone-abc/dns_records/existing-id", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			updated = true
			writeJSON(w, http.StatusOK, cfSingleResponse{
				Result:  cfRecord{ID: "existing-id", Name: "sub.example.com", Type: "A", Content: "9.9.9.9", TTL: 300},
				Success: true,
			})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name:     "sub.example.com",
		Type:     "A",
		Value:    "9.9.9.9",
		TTL:      300,
		Provider: "cloudflare",
	})
	require.NoError(t, err)
	assert.True(t, updated, "expected PUT to /dns_records/:id")
}

func TestCloudflareProvider_UpdateRecord_ListError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"internal error"}]}`))
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "sub.example.com", Type: "A", Value: "1.2.3.4", TTL: 300,
	})
	require.Error(t, err)
}

func TestCloudflareProvider_UpdateRecord_UpdateAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, cfListResponse{
			Result:  []cfRecord{{ID: "r1", Name: "sub.example.com", Type: "A", Content: "1.2.3.4", TTL: 300}},
			Success: true,
		})
	})
	mux.HandleFunc("/zones/zone-abc/dns_records/r1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1234,"message":"bad request"}]}`))
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "sub.example.com", Type: "A", Value: "1.2.3.4", TTL: 300,
	})
	require.Error(t, err)
}

func TestCloudflareProvider_UpdateRecord_CreateAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, cfListResponse{Success: true})
			return
		}
		// POST fails
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1234,"message":"bad request"}]}`))
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "sub.example.com", Type: "A", Value: "1.2.3.4", TTL: 300,
	})
	require.Error(t, err)
}

// --- GetRecord ---

func TestCloudflareProvider_GetRecord_Found(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, cfListResponse{
			Result:  []cfRecord{{ID: "r1", Name: "host.example.com", Type: "A", Content: "10.0.0.1", TTL: 300}},
			Success: true,
		})
	})

	provider := newCloudflareTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "host.example.com", "A")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "host.example.com", rec.Name)
	assert.Equal(t, "10.0.0.1", rec.Value)
	assert.Equal(t, "A", rec.Type)
	assert.Equal(t, "r1", rec.Metadata["cloudflare_id"])
}

func TestCloudflareProvider_GetRecord_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, cfListResponse{Result: nil, Success: true})
	})

	provider := newCloudflareTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "missing.example.com", "A")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestCloudflareProvider_GetRecord_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Invalid access token"}]}`))
	})

	provider := newCloudflareTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "host.example.com", "A")
	require.Error(t, err)
	assert.Nil(t, rec)
}

func TestCloudflareProvider_GetRecord_EmptyType(t *testing.T) {
	mux := http.NewServeMux()
	provider := newCloudflareTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "host.example.com", "")
	require.Error(t, err)
	assert.Nil(t, rec)
	assert.Contains(t, err.Error(), "empty record type")
}

// --- DeleteRecord ---

func TestCloudflareProvider_DeleteRecord_Found(t *testing.T) {
	mux := http.NewServeMux()
	deleted := false

	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, cfListResponse{
			Result:  []cfRecord{{ID: "del-id", Name: "host.example.com", Type: "A", Content: "1.1.1.1", TTL: 300}},
			Success: true,
		})
	})
	mux.HandleFunc("/zones/zone-abc/dns_records/del-id", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			writeJSON(w, http.StatusOK, cfDeleteResponse{
				Result:  map[string]string{"id": "del-id"},
				Success: true,
			})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "host.example.com", "A")
	require.NoError(t, err)
	assert.True(t, deleted, "expected DELETE call")
}

func TestCloudflareProvider_DeleteRecord_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, cfListResponse{Result: nil, Success: true})
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "missing.example.com", "A")
	require.NoError(t, err) // no record → no error
}

func TestCloudflareProvider_DeleteRecord_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"error"}]}`))
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "host.example.com", "A")
	require.Error(t, err)
}

func TestCloudflareProvider_DeleteRecord_DeleteAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, cfListResponse{
			Result:  []cfRecord{{ID: "r1", Name: "host.example.com", Type: "A", Content: "1.1.1.1", TTL: 300}},
			Success: true,
		})
	})
	mux.HandleFunc("/zones/zone-abc/dns_records/r1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"delete failed"}]}`))
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "host.example.com", "A")
	require.Error(t, err)
}

func TestCloudflareProvider_DeleteRecord_EmptyType(t *testing.T) {
	mux := http.NewServeMux()
	provider := newCloudflareTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "host.example.com", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty record type")
}

// --- Validate ---

func TestCloudflareProvider_Validate_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, cfListResponse{Result: nil, Success: true})
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.Validate(context.Background())
	require.NoError(t, err)
}

func TestCloudflareProvider_Validate_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc/dns_records", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Invalid access token"}]}`))
	})

	provider := newCloudflareTestProvider(t, mux)
	err := provider.Validate(context.Background())
	require.Error(t, err)
}
