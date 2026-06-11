package dns_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/internal/dns"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// writeHetznerJSON is a small helper to encode a JSON response for the hcloud API.
func writeHetznerJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

// zoneGetResponse is the minimal shape for GET /zones/:id
type zoneGetResponse struct {
	Zone map[string]interface{} `json:"zone"`
}

// rrsetGetResponse is the minimal shape returned by GET /zones/:id/rrsets/:name/:type
type rrsetGetResponse struct {
	RRSet map[string]interface{} `json:"rrset"`
}

// rrsetCreateResponse is the minimal shape returned by POST /zones/:id/rrsets
type rrsetCreateResponse struct {
	RRSet  map[string]interface{} `json:"rrset"`
	Action map[string]interface{} `json:"action"`
}

// actionResponse is the minimal shape returned by action endpoints (change_ttl, set_records, delete)
type actionResponse struct {
	Action map[string]interface{} `json:"action"`
}

// hetznerErrorResponse is the hcloud error format
type hetznerErrorResponse struct {
	Error map[string]string `json:"error"`
}

func notFoundResponse() hetznerErrorResponse {
	return hetznerErrorResponse{Error: map[string]string{
		"code":    "not_found",
		"message": "resource not found",
	}}
}

// makeZoneJSON returns a minimal valid zone JSON object for mux handlers.
func makeZoneJSON() map[string]interface{} {
	return map[string]interface{}{
		"id":   42,
		"name": "example.com",
		"mode": "primary",
		"authoritative_nameservers": map[string]interface{}{
			"assigned":          []string{},
			"delegated":         []string{},
			"delegation_status": "valid",
		},
		"protection": map[string]interface{}{
			"delete": false,
		},
		"record_count": 0,
		"status":       "ok",
		"ttl":          3600,
	}
}

// makeRRSetJSON returns a minimal valid rrset JSON object.
func makeRRSetJSON(name, rrtype, value string, ttl int) map[string]interface{} {
	return map[string]interface{}{
		"zone": 42,
		"id":   name + "/" + rrtype,
		"name": name,
		"type": rrtype,
		"ttl":  ttl,
		"records": []map[string]interface{}{
			{"value": value, "comment": ""},
		},
	}
}

// newHetznerTestProvider builds a HetznerProvider wired to a fake httptest server.
func newHetznerTestProvider(t *testing.T, mux *http.ServeMux) *dns.HetznerProvider {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	cfg := &config.HetznerConfig{
		APIToken: "test-token",
		ZoneID:   "zone-abc",
	}
	logger := zap.NewNop()

	client := hcloud.NewClient(
		hcloud.WithToken("test-token"),
		hcloud.WithEndpoint(server.URL),
	)
	return dns.NewHetznerProviderWithClient(cfg, client, logger)
}

// --- convertRecordType ---

func TestHetznerProvider_ConvertRecordType_AllSupported(t *testing.T) {
	// We exercise convertRecordType indirectly via GetRecord.
	// Using a zone that returns id=42 (numeric), so RRSet paths use /zones/42/...

	rtypes := []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA"}

	for _, rtype := range rtypes {
		rtype := rtype
		t.Run("GetRecord_"+rtype, func(t *testing.T) {
			mux := http.NewServeMux()

			// Zone.Get by id "zone-abc" → returns zone with numeric id=42
			mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
				writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
			})
			// After zone fetch, SDK uses numeric id from Zone struct for subsequent calls
			// Zone.Get returns id=42, so all subsequent rrset calls use /zones/42/...
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				// Return 404 for any rrset lookup → triggers not-found path
				writeHetznerJSON(w, http.StatusNotFound, notFoundResponse())
			})

			provider := newHetznerTestProvider(t, mux)
			rec, err := provider.GetRecord(context.Background(), "host.example.com", rtype)
			require.NoError(t, err)
			assert.Nil(t, rec) // not found → nil record
		})
	}
}

func TestHetznerProvider_ConvertRecordType_Unsupported(t *testing.T) {
	mux := http.NewServeMux()
	// Zone.Get succeeds
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})

	provider := newHetznerTestProvider(t, mux)

	// "SOA" is not supported in convertRecordType
	rec, err := provider.GetRecord(context.Background(), "example.com", "SOA")
	require.Error(t, err)
	assert.Nil(t, rec)
	assert.Contains(t, err.Error(), "unsupported record type")
}

// --- findRRSet ---
// After Zone.Get returns zone with id=42, RRSet API uses /zones/42/rrsets/...

func TestHetznerProvider_FindRRSet_Found(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	// Zone id=42, so rrset path is /zones/42/rrsets/...
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{RRSet: makeRRSetJSON("host.example.com", "A", "1.2.3.4", 300)})
	})

	provider := newHetznerTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "host.example.com", "A")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "host.example.com", rec.Name)
	assert.Equal(t, "1.2.3.4", rec.Value)
	assert.Equal(t, 300, rec.TTL)
}

func TestHetznerProvider_FindRRSet_NotFound(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/missing.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusNotFound, notFoundResponse())
	})

	provider := newHetznerTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "missing.example.com", "A")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestHetznerProvider_FindRRSet_APIError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusInternalServerError, hetznerErrorResponse{
			Error: map[string]string{"code": "internal_error", "message": "server error"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "host.example.com", "A")
	require.Error(t, err)
	assert.Nil(t, rec)
}

// --- GetRecord with multiple record values (warn path) ---

func TestHetznerProvider_GetRecord_MultipleValues(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		rrset := map[string]interface{}{
			"zone": 42,
			"id":   "host.example.com/A",
			"name": "host.example.com",
			"type": "A",
			"ttl":  300,
			"records": []map[string]interface{}{
				{"value": "1.1.1.1", "comment": ""},
				{"value": "2.2.2.2", "comment": ""},
			},
		}
		writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{RRSet: rrset})
	})

	provider := newHetznerTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "host.example.com", "A")
	require.NoError(t, err)
	require.NotNil(t, rec)
	// Should return first value only
	assert.Equal(t, "1.1.1.1", rec.Value)
}

// GetRecord with nil TTL in RRSet
func TestHetznerProvider_GetRecord_NilTTL(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		// No ttl field → TTL will be nil
		rrset := map[string]interface{}{
			"zone": 42,
			"id":   "host.example.com/A",
			"name": "host.example.com",
			"type": "A",
			"records": []map[string]interface{}{
				{"value": "3.3.3.3", "comment": ""},
			},
		}
		writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{RRSet: rrset})
	})

	provider := newHetznerTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "host.example.com", "A")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, 0, rec.TTL)
}

// GetRecord with no records in RRSet
func TestHetznerProvider_GetRecord_EmptyRecords(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		rrset := map[string]interface{}{
			"zone":    42,
			"id":      "host.example.com/A",
			"name":    "host.example.com",
			"type":    "A",
			"ttl":     300,
			"records": []map[string]interface{}{},
		}
		writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{RRSet: rrset})
	})

	provider := newHetznerTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "host.example.com", "A")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "", rec.Value)
}

// --- updateExistingRRSet ---

func TestHetznerProvider_UpdateRecord_UpdatePath_SameTTL(t *testing.T) {
	// When existing RRSet has same TTL as requested, only set_records should be called
	mux := http.NewServeMux()

	setRecordsCalled := false

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{
			RRSet: makeRRSetJSON("host.example.com", "A", "1.2.3.4", 300),
		})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A/actions/set_records", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			setRecordsCalled = true
			writeHetznerJSON(w, http.StatusOK, actionResponse{Action: map[string]interface{}{"id": 1}})
		}
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name:  "host.example.com",
		Type:  "A",
		Value: "9.9.9.9",
		TTL:   300, // same TTL
	})
	require.NoError(t, err)
	assert.True(t, setRecordsCalled)
}

func TestHetznerProvider_UpdateRecord_UpdatePath_DifferentTTL(t *testing.T) {
	// When TTL differs, change_ttl and set_records should both be called
	mux := http.NewServeMux()

	changeTTLCalled := false
	setRecordsCalled := false

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{
			RRSet: makeRRSetJSON("host.example.com", "A", "1.2.3.4", 300),
		})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A/actions/change_ttl", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			changeTTLCalled = true
			writeHetznerJSON(w, http.StatusOK, actionResponse{Action: map[string]interface{}{"id": 1}})
		}
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A/actions/set_records", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			setRecordsCalled = true
			writeHetznerJSON(w, http.StatusOK, actionResponse{Action: map[string]interface{}{"id": 2}})
		}
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name:  "host.example.com",
		Type:  "A",
		Value: "9.9.9.9",
		TTL:   600, // different TTL
	})
	require.NoError(t, err)
	assert.True(t, changeTTLCalled)
	assert.True(t, setRecordsCalled)
}

func TestHetznerProvider_UpdateRecord_UpdatePath_NilTTL(t *testing.T) {
	// When existing RRSet TTL is nil, change_ttl should be called
	mux := http.NewServeMux()

	changeTTLCalled := false

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		// TTL is omitted → nil in parsed struct
		rrset := map[string]interface{}{
			"zone": 42,
			"id":   "host.example.com/A",
			"name": "host.example.com",
			"type": "A",
			"records": []map[string]interface{}{
				{"value": "1.2.3.4", "comment": ""},
			},
		}
		writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{RRSet: rrset})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A/actions/change_ttl", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			changeTTLCalled = true
			writeHetznerJSON(w, http.StatusOK, actionResponse{Action: map[string]interface{}{"id": 1}})
		}
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A/actions/set_records", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, actionResponse{Action: map[string]interface{}{"id": 2}})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "host.example.com", Type: "A", Value: "9.9.9.9", TTL: 300,
	})
	require.NoError(t, err)
	assert.True(t, changeTTLCalled)
}

func TestHetznerProvider_UpdateRecord_UpdatePath_ChangeTTLError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{
			RRSet: makeRRSetJSON("host.example.com", "A", "1.2.3.4", 100),
		})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A/actions/change_ttl", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusInternalServerError, hetznerErrorResponse{
			Error: map[string]string{"code": "internal_error", "message": "change_ttl failed"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "host.example.com", Type: "A", Value: "9.9.9.9", TTL: 300,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update RRSet TTL")
}

func TestHetznerProvider_UpdateRecord_UpdatePath_SetRecordsError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{
			RRSet: makeRRSetJSON("host.example.com", "A", "1.2.3.4", 300),
		})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A/actions/set_records", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusInternalServerError, hetznerErrorResponse{
			Error: map[string]string{"code": "internal_error", "message": "set_records failed"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "host.example.com", Type: "A", Value: "9.9.9.9", TTL: 300,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update RRSet records")
}

// --- createNewRRSet ---

func TestHetznerProvider_UpdateRecord_CreatePath(t *testing.T) {
	mux := http.NewServeMux()

	createCalled := false

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	// RRSet not found
	mux.HandleFunc("/zones/42/rrsets/new.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusNotFound, notFoundResponse())
	})
	// Create RRSet
	mux.HandleFunc("/zones/42/rrsets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			createCalled = true
			writeHetznerJSON(w, http.StatusOK, rrsetCreateResponse{
				RRSet:  makeRRSetJSON("new.example.com", "A", "5.6.7.8", 300),
				Action: map[string]interface{}{"id": 1},
			})
		}
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "new.example.com", Type: "A", Value: "5.6.7.8", TTL: 300,
	})
	require.NoError(t, err)
	assert.True(t, createCalled)
}

func TestHetznerProvider_UpdateRecord_CreatePath_Error(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/new.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusNotFound, notFoundResponse())
	})
	mux.HandleFunc("/zones/42/rrsets", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusInternalServerError, hetznerErrorResponse{
			Error: map[string]string{"code": "internal_error", "message": "create failed"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "new.example.com", Type: "A", Value: "5.6.7.8", TTL: 300,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create RRSet")
}

func TestHetznerProvider_UpdateRecord_CreatePath_InvalidInput(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/new.example.com/AAAA", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusNotFound, notFoundResponse())
	})
	mux.HandleFunc("/zones/42/rrsets", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusUnprocessableEntity, hetznerErrorResponse{
			Error: map[string]string{"code": "invalid_input", "message": "invalid"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "new.example.com", Type: "AAAA", Value: "::1", TTL: 300,
	})
	require.Error(t, err)
}

// --- deleteRRSet ---

func TestHetznerProvider_DeleteRecord_Found(t *testing.T) {
	mux := http.NewServeMux()

	deleteCalled := false

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{
				RRSet: makeRRSetJSON("host.example.com", "A", "1.2.3.4", 300),
			})
			return
		}
		if r.Method == http.MethodDelete {
			deleteCalled = true
			writeHetznerJSON(w, http.StatusOK, actionResponse{Action: map[string]interface{}{"id": 1}})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "host.example.com", "A")
	require.NoError(t, err)
	assert.True(t, deleteCalled)
}

func TestHetznerProvider_DeleteRecord_NotFound(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/missing.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusNotFound, notFoundResponse())
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "missing.example.com", "A")
	require.NoError(t, err) // not found → no error
}

func TestHetznerProvider_DeleteRecord_DeleteAPIError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeHetznerJSON(w, http.StatusOK, rrsetGetResponse{
				RRSet: makeRRSetJSON("host.example.com", "A", "1.2.3.4", 300),
			})
			return
		}
		if r.Method == http.MethodDelete {
			writeHetznerJSON(w, http.StatusInternalServerError, hetznerErrorResponse{
				Error: map[string]string{"code": "internal_error", "message": "delete failed"},
			})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "host.example.com", "A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete RRSet")
}

// --- UpdateRecord error paths ---

func TestHetznerProvider_UpdateRecord_GetZoneError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusUnauthorized, hetznerErrorResponse{
			Error: map[string]string{"code": "unauthorized", "message": "unauthorized"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "host.example.com", Type: "A", Value: "1.2.3.4", TTL: 300,
	})
	require.Error(t, err)
}

func TestHetznerProvider_UpdateRecord_ConvertRecordTypeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "host.example.com", Type: "SOA", Value: "something", TTL: 300,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported record type")
}

func TestHetznerProvider_UpdateRecord_FindRRSetError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusInternalServerError, hetznerErrorResponse{
			Error: map[string]string{"code": "internal_error", "message": "find failed"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.UpdateRecord(context.Background(), interfaces.DNSRecord{
		Name: "host.example.com", Type: "A", Value: "1.2.3.4", TTL: 300,
	})
	require.Error(t, err)
}

// --- GetRecord error paths ---

func TestHetznerProvider_GetRecord_GetZoneError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusInternalServerError, hetznerErrorResponse{
			Error: map[string]string{"code": "internal_error", "message": "zone error"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	rec, err := provider.GetRecord(context.Background(), "host.example.com", "A")
	require.Error(t, err)
	assert.Nil(t, rec)
}

// --- DeleteRecord error paths ---

func TestHetznerProvider_DeleteRecord_GetZoneError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusInternalServerError, hetznerErrorResponse{
			Error: map[string]string{"code": "internal_error", "message": "zone error"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "host.example.com", "A")
	require.Error(t, err)
}

func TestHetznerProvider_DeleteRecord_ConvertRecordTypeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "host.example.com", "SOA")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported record type")
}

func TestHetznerProvider_DeleteRecord_FindRRSetError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})
	mux.HandleFunc("/zones/42/rrsets/host.example.com/A", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusInternalServerError, hetznerErrorResponse{
			Error: map[string]string{"code": "internal_error", "message": "find failed"},
		})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.DeleteRecord(context.Background(), "host.example.com", "A")
	require.Error(t, err)
}

// --- Validate with mock ---

func TestHetznerProvider_Validate_WithMock_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})

	provider := newHetznerTestProvider(t, mux)
	err := provider.Validate(context.Background())
	require.NoError(t, err)
}

func TestHetznerProvider_Validate_WithMock_CachedZone(t *testing.T) {
	// Second Validate call should use cached zone (no second HTTP call)
	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/zone-abc", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		writeHetznerJSON(w, http.StatusOK, zoneGetResponse{Zone: makeZoneJSON()})
	})

	provider := newHetznerTestProvider(t, mux)
	require.NoError(t, provider.Validate(context.Background()))
	require.NoError(t, provider.Validate(context.Background()))
	assert.Equal(t, 1, callCount, "zone should be cached after first call")
}
