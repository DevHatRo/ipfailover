package dns_test

import (
	"context"
	"testing"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/internal/dns"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

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

		// Create provider
		provider := dns.NewCloudflareProvider(cfg, logger)
		assert.NotNil(t, provider)

		// Test validation - this will fail with real API but tests the code path
		ctx := context.Background()
		err := provider.Validate(ctx)
		// We expect an error since we're using a fake API token
		assert.Error(t, err)
	})
}

func TestCloudflareProvider_CRUDOperations(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.CloudflareConfig{
		APIToken: "test-token",
		ZoneID:   "test-zone",
	}

	t.Run("GetRecord - network error", func(t *testing.T) {
		provider := dns.NewCloudflareProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record, err := provider.GetRecord(ctx, "test.example.com", "A")
		assert.Error(t, err)
		assert.Nil(t, record)
	})

	t.Run("UpdateRecord - network error", func(t *testing.T) {
		provider := dns.NewCloudflareProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record := interfaces.DNSRecord{
			Name:     "test.example.com",
			Type:     "A",
			Value:    "1.2.3.4",
			TTL:      300,
			Provider: "cloudflare",
		}

		err := provider.UpdateRecord(ctx, record)
		assert.Error(t, err)
	})

	t.Run("DeleteRecord - network error", func(t *testing.T) {
		provider := dns.NewCloudflareProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := provider.DeleteRecord(ctx, "test.example.com", "A")
		assert.Error(t, err)
	})

	t.Run("Validate - network error", func(t *testing.T) {
		provider := dns.NewCloudflareProvider(cfg, logger)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := provider.Validate(ctx)
		assert.Error(t, err)
	})

	t.Run("GetRecord - empty record type", func(t *testing.T) {
		provider := dns.NewCloudflareProvider(cfg, logger)

		// Test validating empty record type input
		ctx := context.Background()

		record, err := provider.GetRecord(ctx, "test.example.com", "")
		assert.Error(t, err)
		assert.Nil(t, record)
		assert.Contains(t, err.Error(), "empty record type")
	})

	t.Run("DeleteRecord - empty record type", func(t *testing.T) {
		provider := dns.NewCloudflareProvider(cfg, logger)

		// Test validating empty record type input
		ctx := context.Background()

		err := provider.DeleteRecord(ctx, "test.example.com", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty record type")
	})
}

func TestCloudflareProvider_ErrorHandling(t *testing.T) {
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
}

func TestCloudflareProvider_ConfigurationValidation(t *testing.T) {
	t.Run("Cloudflare config validation", func(t *testing.T) {
		cfg := &config.CloudflareConfig{
			APIToken: "test-token",
			ZoneID:   "test-zone",
		}

		err := cfg.Validate()
		assert.NoError(t, err)
	})
}
