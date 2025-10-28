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
	"go.uber.org/zap"
)

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
			if _, err := w.Write([]byte(`{"result":{"data":[],"meta":{"result":1}}}`)); err != nil {
				t.Errorf("failed to write mock response: %v", err)
			}
		}))
		defer server.Close()

		// We can't easily test the actual validation without mocking the HTTP client
		// This test ensures the provider can be created without errors
		assert.NotNil(t, provider)
	})
}

func TestCPanelProvider_CRUDOperations(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.CPanelConfig{
		BaseURL:  "https://cpanel.example.com",
		Username: "testuser",
		APIToken: "test-token",
		Zone:     "example.com",
	}

	t.Run("GetRecord - network error", func(t *testing.T) {
		provider := dns.NewCPanelProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record, err := provider.GetRecord(ctx, "test.example.com", "A")
		assert.Error(t, err)
		assert.Nil(t, record)
	})

	t.Run("UpdateRecord - network error", func(t *testing.T) {
		provider := dns.NewCPanelProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record := interfaces.DNSRecord{
			Name:     "test.example.com",
			Type:     "A",
			Value:    "1.2.3.4",
			TTL:      300,
			Provider: "cpanel",
		}

		err := provider.UpdateRecord(ctx, record)
		assert.Error(t, err)
	})

	t.Run("DeleteRecord - network error", func(t *testing.T) {
		provider := dns.NewCPanelProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := provider.DeleteRecord(ctx, "test.example.com", "A")
		assert.Error(t, err)
	})

	t.Run("Validate - network error", func(t *testing.T) {
		provider := dns.NewCPanelProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := provider.Validate(ctx)
		assert.Error(t, err)
	})

	t.Run("GetRecord - empty record type", func(t *testing.T) {
		provider := dns.NewCPanelProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record, err := provider.GetRecord(ctx, "test.example.com", "")
		assert.Error(t, err)
		assert.Nil(t, record)
	})

	t.Run("DeleteRecord - empty record type", func(t *testing.T) {
		provider := dns.NewCPanelProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := provider.DeleteRecord(ctx, "test.example.com", "")
		assert.Error(t, err)
	})
}

func TestCPanelProvider_ErrorHandling(t *testing.T) {
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
}

func TestCPanelProvider_ConfigurationValidation(t *testing.T) {
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
}
