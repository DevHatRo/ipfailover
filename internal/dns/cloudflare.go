package dns

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cloudflare/cloudflare-go/v2"
	"github.com/cloudflare/cloudflare-go/v2/dns"
	"github.com/cloudflare/cloudflare-go/v2/option"
	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
)

// CloudflareProvider implements DNSProvider for Cloudflare
type CloudflareProvider struct {
	config *config.CloudflareConfig
	client *cloudflare.Client
	logger *zap.Logger
}

// NewCloudflareProvider creates a new Cloudflare DNS provider
func NewCloudflareProvider(cfg *config.CloudflareConfig, logger *zap.Logger) *CloudflareProvider {
	if cfg == nil {
		if logger != nil {
			logger.Error("cloudflare config is nil")
		}
		return nil
	}

	client := cloudflare.NewClient(
		option.WithAPIToken(cfg.APIToken),
	)

	return &CloudflareProvider{
		config: cfg,
		client: client,
		logger: logger,
	}
}

// NewCloudflareProviderWithClient creates a new Cloudflare DNS provider with a custom API client
func NewCloudflareProviderWithClient(cfg *config.CloudflareConfig, client *cloudflare.Client, logger *zap.Logger) *CloudflareProvider {
	if cfg == nil {
		if logger != nil {
			logger.Error("cloudflare config is nil")
		}
		return nil
	}

	if client == nil {
		client = cloudflare.NewClient(
			option.WithAPIToken(cfg.APIToken),
		)
	}

	return &CloudflareProvider{
		config: cfg,
		client: client,
		logger: logger,
	}
}

// Name returns the provider name
func (c *CloudflareProvider) Name() string {
	return "cloudflare"
}

// createRecordParam creates the appropriate RecordUnionParam based on the record type
func (c *CloudflareProvider) createRecordParam(record interfaces.DNSRecord) (dns.RecordUnionParam, error) {
	switch record.Type {
	case "A":
		return dns.ARecordParam{
			Name:    cloudflare.String(record.Name),
			Type:    cloudflare.Raw[dns.ARecordType](dns.ARecordType(record.Type)),
			Content: cloudflare.String(record.Value),
			TTL:     cloudflare.Raw[dns.TTL](dns.TTL(record.TTL)),
			Proxied: cloudflare.Bool(c.config.Proxied),
		}, nil
	case "AAAA":
		return dns.AAAARecordParam{
			Name:    cloudflare.String(record.Name),
			Type:    cloudflare.Raw[dns.AAAARecordType](dns.AAAARecordType(record.Type)),
			Content: cloudflare.String(record.Value),
			TTL:     cloudflare.Raw[dns.TTL](dns.TTL(record.TTL)),
			Proxied: cloudflare.Bool(c.config.Proxied),
		}, nil
	case "CNAME":
		return dns.CNAMERecordParam{
			Name:    cloudflare.String(record.Name),
			Type:    cloudflare.Raw[dns.CNAMERecordType](dns.CNAMERecordType(record.Type)),
			Content: cloudflare.F[interface{}](record.Value),
			TTL:     cloudflare.Raw[dns.TTL](dns.TTL(record.TTL)),
			Proxied: cloudflare.Bool(c.config.Proxied),
		}, nil
	case "TXT":
		return dns.TXTRecordParam{
			Name:    cloudflare.String(record.Name),
			Type:    cloudflare.Raw[dns.TXTRecordType](dns.TXTRecordType(record.Type)),
			Content: cloudflare.String(record.Value),
			TTL:     cloudflare.Raw[dns.TTL](dns.TTL(record.TTL)),
		}, nil
	case "MX":
		// For MX records, we need to extract priority from the value or metadata
		priority := 10 // Default priority
		if priorityStr, exists := record.Metadata["priority"]; exists {
			if p, err := strconv.ParseInt(priorityStr, 10, 64); err == nil {
				priority = int(p)
			} else {
				// Log warning on parse failure and fall back to default priority
				zap.L().Warn("Failed to parse MX priority from metadata, using default",
					zap.String("priority_str", priorityStr),
					zap.Error(err),
					zap.Int("default_priority", priority))
			}
		}
		return dns.MXRecordParam{
			Name:     cloudflare.String(record.Name),
			Type:     cloudflare.Raw[dns.MXRecordType](dns.MXRecordType(record.Type)),
			Content:  cloudflare.String(record.Value),
			TTL:      cloudflare.Raw[dns.TTL](dns.TTL(record.TTL)),
			Priority: cloudflare.Raw[float64](float64(priority)),
		}, nil
	case "NS":
		return dns.NSRecordParam{
			Name:    cloudflare.String(record.Name),
			Type:    cloudflare.Raw[dns.NSRecordType](dns.NSRecordType(record.Type)),
			Content: cloudflare.String(record.Value),
			TTL:     cloudflare.Raw[dns.TTL](dns.TTL(record.TTL)),
		}, nil
	case "PTR":
		return dns.PTRRecordParam{
			Name:    cloudflare.String(record.Name),
			Type:    cloudflare.Raw[dns.PTRRecordType](dns.PTRRecordType(record.Type)),
			Content: cloudflare.String(record.Value),
			TTL:     cloudflare.Raw[dns.TTL](dns.TTL(record.TTL)),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported DNS record type: %s", record.Type)
	}
}

// UpdateRecord updates or creates a DNS record
func (c *CloudflareProvider) UpdateRecord(ctx context.Context, record interfaces.DNSRecord) error {
	c.logger.Info("updating DNS record",
		zap.String("provider", "cloudflare"),
		zap.String("record", record.Name),
		zap.String("type", record.Type),
		zap.String("value", record.Value),
	)

	// First, try to find existing record
	records, err := c.client.DNS.Records.List(ctx, dns.RecordListParams{
		ZoneID: cloudflare.String(c.config.ZoneID),
		Name:   cloudflare.String(record.Name),
		Type:   cloudflare.Raw[dns.RecordListParamsType](dns.RecordListParamsType(record.Type)),
	})
	if err != nil {
		return errors.NewDNSProviderError("cloudflare", record.Name, err)
	}

	if len(records.Result) > 0 {
		// Update existing record
		existingRecord := records.Result[0]
		recordParam, err := c.createRecordParam(record)
		if err != nil {
			return errors.NewDNSProviderError("cloudflare", record.Name, err)
		}
		_, err = c.client.DNS.Records.Update(ctx, existingRecord.ID, dns.RecordUpdateParams{
			ZoneID: cloudflare.String(c.config.ZoneID),
			Record: recordParam,
		})
		if err != nil {
			return errors.NewDNSProviderError("cloudflare", record.Name, err)
		}

		c.logger.Info("DNS record updated successfully",
			zap.String("provider", "cloudflare"),
			zap.String("record", record.Name),
			zap.String("record_id", existingRecord.ID),
		)
		return nil
	}

	// Create new record
	recordParam, err := c.createRecordParam(record)
	if err != nil {
		return errors.NewDNSProviderError("cloudflare", record.Name, err)
	}
	_, err = c.client.DNS.Records.New(ctx, dns.RecordNewParams{
		ZoneID: cloudflare.String(c.config.ZoneID),
		Record: recordParam,
	})
	if err != nil {
		return errors.NewDNSProviderError("cloudflare", record.Name, err)
	}

	c.logger.Info("DNS record created successfully",
		zap.String("provider", "cloudflare"),
		zap.String("record", record.Name),
	)

	return nil
}

// GetRecord retrieves an existing DNS record
func (c *CloudflareProvider) GetRecord(ctx context.Context, name string, rtype string) (*interfaces.DNSRecord, error) {
	c.logger.Debug("getting DNS record",
		zap.String("provider", "cloudflare"),
		zap.String("record", name),
		zap.String("type", rtype),
	)

	// Validate record type is not empty
	if rtype == "" {
		return nil, errors.NewDNSProviderError("cloudflare", name, fmt.Errorf("empty record type"))
	}

	records, err := c.client.DNS.Records.List(ctx, dns.RecordListParams{
		ZoneID: cloudflare.String(c.config.ZoneID),
		Name:   cloudflare.String(name),
		Type:   cloudflare.Raw[dns.RecordListParamsType](dns.RecordListParamsType(rtype)),
	})
	if err != nil {
		return nil, errors.NewDNSProviderError("cloudflare", name, err)
	}

	if len(records.Result) == 0 {
		return nil, nil // Record not found
	}

	// Return the first matching record
	record := records.Result[0]
	return &interfaces.DNSRecord{
		Name:     record.Name,
		Type:     string(record.Type),
		Value:    record.Content.(string),
		TTL:      int(record.TTL),
		Provider: "cloudflare",
		Metadata: map[string]string{
			"cloudflare_id": record.ID,
			"proxied":       fmt.Sprintf("%t", record.Proxied),
		},
	}, nil
}

// DeleteRecord deletes a DNS record
func (c *CloudflareProvider) DeleteRecord(ctx context.Context, name, recordType string) error {
	c.logger.Info("deleting DNS record",
		zap.String("provider", "cloudflare"),
		zap.String("record", name),
		zap.String("type", recordType),
	)

	// Validate record type is not empty
	if recordType == "" {
		return errors.NewDNSProviderError("cloudflare", name, fmt.Errorf("empty record type"))
	}

	records, err := c.client.DNS.Records.List(ctx, dns.RecordListParams{
		ZoneID: cloudflare.String(c.config.ZoneID),
		Name:   cloudflare.String(name),
		Type:   cloudflare.Raw[dns.RecordListParamsType](dns.RecordListParamsType(recordType)),
	})
	if err != nil {
		return errors.NewDNSProviderError("cloudflare", name, err)
	}

	if len(records.Result) == 0 {
		c.logger.Warn("record not found for deletion",
			zap.String("provider", "cloudflare"),
			zap.String("record", name),
			zap.String("type", recordType),
		)
		return nil // Record doesn't exist, consider it deleted
	}

	// Delete the first matching record
	record := records.Result[0]
	_, err = c.client.DNS.Records.Delete(ctx, record.ID, dns.RecordDeleteParams{
		ZoneID: cloudflare.String(c.config.ZoneID),
	})
	if err != nil {
		return errors.NewDNSProviderError("cloudflare", name, err)
	}

	c.logger.Info("DNS record deleted successfully",
		zap.String("provider", "cloudflare"),
		zap.String("record", name),
		zap.String("record_id", record.ID),
	)

	return nil
}

// Validate checks if the provider configuration is valid
func (c *CloudflareProvider) Validate(ctx context.Context) error {
	c.logger.Debug("validating Cloudflare provider configuration")

	// Test API access by listing records
	_, err := c.client.DNS.Records.List(ctx, dns.RecordListParams{
		ZoneID: cloudflare.String(c.config.ZoneID),
	})
	if err != nil {
		return errors.NewDNSProviderError("cloudflare", "validation", err)
	}

	c.logger.Info("Cloudflare provider validation successful")
	return nil
}
