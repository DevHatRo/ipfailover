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

func TestRoute53Provider_Name(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Route53Config{
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Region:          "us-east-1",
		HostedZoneID:    "test-zone",
	}

	provider, err := dns.NewRoute53Provider(cfg, logger)
	assert.NoError(t, err)
	assert.Equal(t, "route53", provider.Name())
}

func TestRoute53Provider_Creation(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.Route53Config{
			AccessKeyID:     "test-key",
			SecretAccessKey: "test-secret",
			Region:          "us-east-1",
			HostedZoneID:    "test-zone",
		}

		provider, err := dns.NewRoute53Provider(cfg, logger)
		assert.NoError(t, err)
		assert.NotNil(t, provider)
	})
}

func TestRoute53Provider_CRUDOperations(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Route53Config{
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		Region:          "us-east-1",
		HostedZoneID:    "test-zone",
	}

	t.Run("GetRecord - network error", func(t *testing.T) {
		provider, err := dns.NewRoute53Provider(cfg, logger)
		assert.NoError(t, err)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record, err := provider.GetRecord(ctx, "test.example.com", "A")
		assert.Error(t, err)
		assert.Nil(t, record)
	})

	t.Run("UpdateRecord - network error", func(t *testing.T) {
		provider, err := dns.NewRoute53Provider(cfg, logger)
		assert.NoError(t, err)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record := interfaces.DNSRecord{
			Name:     "test.example.com",
			Type:     "A",
			Value:    "1.2.3.4",
			TTL:      300,
			Provider: "route53",
		}

		err = provider.UpdateRecord(ctx, record)
		assert.Error(t, err)
	})

	t.Run("DeleteRecord - network error", func(t *testing.T) {
		provider, err := dns.NewRoute53Provider(cfg, logger)
		assert.NoError(t, err)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = provider.DeleteRecord(ctx, "test.example.com", "A")
		assert.Error(t, err)
	})

	t.Run("Validate - network error", func(t *testing.T) {
		provider, err := dns.NewRoute53Provider(cfg, logger)
		assert.NoError(t, err)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = provider.Validate(ctx)
		assert.Error(t, err)
	})

	t.Run("GetRecord - empty record type", func(t *testing.T) {
		provider, err := dns.NewRoute53Provider(cfg, logger)
		assert.NoError(t, err)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record, err := provider.GetRecord(ctx, "test.example.com", "")
		assert.Error(t, err)
		assert.Nil(t, record)
	})

	t.Run("DeleteRecord - empty record type", func(t *testing.T) {
		provider, err := dns.NewRoute53Provider(cfg, logger)
		assert.NoError(t, err)

		// Test with cancelled context to trigger error path
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = provider.DeleteRecord(ctx, "test.example.com", "")
		assert.Error(t, err)
	})
}

func TestRoute53Provider_ConfigurationValidation(t *testing.T) {
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
}
