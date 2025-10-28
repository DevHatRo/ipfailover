package dns

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"go.uber.org/zap"
)

// HetznerProvider implements DNSProvider for Hetzner using the official hcloud-go SDK
type HetznerProvider struct {
	config *config.HetznerConfig
	client *hcloud.Client
	logger *zap.Logger
	zone   *hcloud.Zone
	zoneMu sync.RWMutex
}

// NewHetznerProvider creates a new Hetzner DNS provider using the official hcloud-go SDK
func NewHetznerProvider(cfg *config.HetznerConfig, logger *zap.Logger) *HetznerProvider {
	if cfg == nil {
		if logger != nil {
			logger.Error("hetzner config is nil")
		}
		return nil
	}

	// Validate API token
	token := strings.TrimSpace(cfg.APIToken)
	if token == "" {
		if logger != nil {
			logger.Error("hetzner API token is empty")
		}
		return nil
	}

	client := hcloud.NewClient(hcloud.WithToken(token))

	return &HetznerProvider{
		config: cfg,
		client: client,
		logger: logger,
	}
}

// NewHetznerProviderWithClient creates a new Hetzner DNS provider with a custom SDK client
func NewHetznerProviderWithClient(cfg *config.HetznerConfig, client *hcloud.Client, logger *zap.Logger) *HetznerProvider {
	if cfg == nil {
		if logger != nil {
			logger.Error("hetzner config is nil")
		}
		return nil
	}

	if client == nil {
		if cfg.APIToken == "" {
			if logger != nil {
				logger.Error("hetzner API token is empty")
			}
			return nil
		}
		client = hcloud.NewClient(hcloud.WithToken(cfg.APIToken))
	}

	return &HetznerProvider{
		config: cfg,
		client: client,
		logger: logger,
	}
}

// Name returns the provider name
func (h *HetznerProvider) Name() string {
	return "hetzner"
}

// UpdateRecord updates or creates a DNS record
func (h *HetznerProvider) UpdateRecord(ctx context.Context, record interfaces.DNSRecord) error {
	h.logger.Info("updating DNS record",
		zap.String("provider", "hetzner"),
		zap.String("record", record.Name),
		zap.String("type", record.Type),
		zap.String("value", record.Value),
	)

	// Get or cache the zone
	zone, err := h.getZone(ctx)
	if err != nil {
		return errors.NewDNSProviderError("hetzner", record.Name, err)
	}

	// Convert record type to hcloud ZoneRRSetType
	rrsetType, err := h.convertRecordType(record.Type)
	if err != nil {
		return errors.NewDNSProviderError("hetzner", record.Name, err)
	}

	// First, try to find existing RRSet
	existingRRSet, err := h.findRRSet(ctx, zone, record.Name, rrsetType)
	if err != nil {
		return errors.NewDNSProviderError("hetzner", record.Name, err)
	}

	if existingRRSet != nil {
		// Update existing RRSet
		return h.updateExistingRRSet(ctx, existingRRSet, record)
	}

	// Create new RRSet
	return h.createNewRRSet(ctx, zone, record)
}

// GetRecord retrieves an existing DNS record
func (h *HetznerProvider) GetRecord(ctx context.Context, name string, rtype string) (*interfaces.DNSRecord, error) {
	h.logger.Debug("getting DNS record",
		zap.String("provider", "hetzner"),
		zap.String("record", name),
		zap.String("type", rtype),
	)

	// Get or cache the zone
	zone, err := h.getZone(ctx)
	if err != nil {
		return nil, errors.NewDNSProviderError("hetzner", name, err)
	}

	// Convert record type to hcloud ZoneRRSetType
	rrsetType, err := h.convertRecordType(rtype)
	if err != nil {
		return nil, errors.NewDNSProviderError("hetzner", name, err)
	}

	rrset, err := h.findRRSet(ctx, zone, name, rrsetType)
	if err != nil {
		return nil, errors.NewDNSProviderError("hetzner", name, err)
	}

	if rrset == nil {
		return nil, nil // Record not found
	}

	// Get the first record value (assuming single value for simplicity)
	var value string
	if len(rrset.Records) > 0 {
		value = rrset.Records[0].Value

		// Warn if multiple record values exist
		if len(rrset.Records) > 1 {
			h.logger.Warn("multiple record values detected, using first value only",
				zap.String("provider", "hetzner"),
				zap.String("rrset_name", rrset.Name),
				zap.String("rrset_id", rrset.ID),
				zap.Int("record_count", len(rrset.Records)),
				zap.String("used_value", value),
			)
		}
	}

	var ttl int
	if rrset.TTL != nil {
		ttl = *rrset.TTL
	}

	return &interfaces.DNSRecord{
		Name:     rrset.Name,
		Type:     string(rrset.Type),
		Value:    value,
		TTL:      ttl,
		Provider: "hetzner",
		Metadata: map[string]string{
			"rrset_id": rrset.ID,
			"zone_id":  h.config.ZoneID,
		},
	}, nil
}

// DeleteRecord deletes a DNS record
func (h *HetznerProvider) DeleteRecord(ctx context.Context, name, recordType string) error {
	h.logger.Info("deleting DNS record",
		zap.String("provider", "hetzner"),
		zap.String("record", name),
		zap.String("type", recordType),
	)

	// Get or cache the zone
	zone, err := h.getZone(ctx)
	if err != nil {
		return errors.NewDNSProviderError("hetzner", name, err)
	}

	// Convert record type to hcloud ZoneRRSetType
	rrsetType, err := h.convertRecordType(recordType)
	if err != nil {
		return errors.NewDNSProviderError("hetzner", name, err)
	}

	rrset, err := h.findRRSet(ctx, zone, name, rrsetType)
	if err != nil {
		return errors.NewDNSProviderError("hetzner", name, err)
	}

	if rrset == nil {
		h.logger.Warn("record not found for deletion",
			zap.String("provider", "hetzner"),
			zap.String("record", name),
			zap.String("type", recordType),
		)
		return nil // Record doesn't exist, consider it deleted
	}

	if err := h.deleteRRSet(ctx, rrset); err != nil {
		return errors.NewDNSProviderError("hetzner", name, err)
	}

	return nil
}

// Validate checks if the provider configuration is valid
func (h *HetznerProvider) Validate(ctx context.Context) error {
	h.logger.Debug("validating Hetzner provider configuration")

	// Test API access by getting the zone
	_, err := h.getZone(ctx)
	if err != nil {
		return fmt.Errorf("hetzner API validation failed: %w", err)
	}

	h.logger.Info("Hetzner provider validation successful")
	return nil
}

// getZone gets or caches the zone
func (h *HetznerProvider) getZone(ctx context.Context) (*hcloud.Zone, error) {
	// Take read lock to check cached zone
	h.zoneMu.RLock()
	if h.zone != nil {
		zone := h.zone
		h.zoneMu.RUnlock()
		return zone, nil
	}
	h.zoneMu.RUnlock()

	// Zone is nil, acquire write lock
	h.zoneMu.Lock()
	defer h.zoneMu.Unlock()

	// Re-check zone to avoid TOCTOU (Time-of-Check-Time-of-Use)
	if h.zone != nil {
		return h.zone, nil
	}

	// Fetch zone from API
	zone, _, err := h.client.Zone.Get(ctx, h.config.ZoneID)
	if err != nil {
		return nil, fmt.Errorf("failed to get zone: %w", err)
	}

	// Cache the zone
	h.zone = zone
	return zone, nil
}

// convertRecordType converts string record type to hcloud ZoneRRSetType
func (h *HetznerProvider) convertRecordType(recordType string) (hcloud.ZoneRRSetType, error) {
	switch recordType {
	case "A":
		return hcloud.ZoneRRSetTypeA, nil
	case "AAAA":
		return hcloud.ZoneRRSetTypeAAAA, nil
	case "CNAME":
		return hcloud.ZoneRRSetTypeCNAME, nil
	case "MX":
		return hcloud.ZoneRRSetTypeMX, nil
	case "TXT":
		return hcloud.ZoneRRSetTypeTXT, nil
	case "NS":
		return hcloud.ZoneRRSetTypeNS, nil
	case "SRV":
		return hcloud.ZoneRRSetTypeSRV, nil
	case "CAA":
		return hcloud.ZoneRRSetTypeCAA, nil
	default:
		return "", fmt.Errorf("unsupported record type: %s", recordType)
	}
}

// findRRSet finds a RRSet by name and type
func (h *HetznerProvider) findRRSet(ctx context.Context, zone *hcloud.Zone, name string, rrsetType hcloud.ZoneRRSetType) (*hcloud.ZoneRRSet, error) {
	rrset, _, err := h.client.Zone.GetRRSetByNameAndType(ctx, zone, name, rrsetType)
	if err != nil {
		// Check if it's a "not found" error
		if hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
			return nil, nil // RRSet not found
		}
		return nil, fmt.Errorf("failed to get RRSet: %w", err)
	}

	return rrset, nil
}

// updateExistingRRSet updates an existing RRSet
func (h *HetznerProvider) updateExistingRRSet(ctx context.Context, rrset *hcloud.ZoneRRSet, record interfaces.DNSRecord) error {
	// Check if TTL needs to be updated
	if rrset.TTL == nil || *rrset.TTL != record.TTL {
		_, _, err := h.client.Zone.ChangeRRSetTTL(ctx, rrset, hcloud.ZoneRRSetChangeTTLOpts{
			TTL: &record.TTL,
		})
		if err != nil {
			return fmt.Errorf("failed to update RRSet TTL: %w", err)
		}
	}

	// Set the records to the new value
	_, _, err := h.client.Zone.SetRRSetRecords(ctx, rrset, hcloud.ZoneRRSetSetRecordsOpts{
		Records: []hcloud.ZoneRRSetRecord{
			{
				Value:   record.Value,
				Comment: "",
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to update RRSet records: %w", err)
	}

	h.logger.Info("DNS record updated successfully",
		zap.String("provider", "hetzner"),
		zap.String("record", record.Name),
		zap.String("rrset_id", rrset.ID),
		zap.Int("ttl", record.TTL),
	)

	return nil
}

// createNewRRSet creates a new RRSet
func (h *HetznerProvider) createNewRRSet(ctx context.Context, zone *hcloud.Zone, record interfaces.DNSRecord) error {
	rrsetType, err := h.convertRecordType(record.Type)
	if err != nil {
		return fmt.Errorf("failed to convert record type: %w", err)
	}

	_, _, err = h.client.Zone.CreateRRSet(ctx, zone, hcloud.ZoneRRSetCreateOpts{
		Name: record.Name,
		Type: rrsetType,
		TTL:  &record.TTL,
		Records: []hcloud.ZoneRRSetRecord{
			{
				Value:   record.Value,
				Comment: "",
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create RRSet: %w", err)
	}

	h.logger.Info("DNS record created successfully",
		zap.String("provider", "hetzner"),
		zap.String("record", record.Name),
	)

	return nil
}

// deleteRRSet deletes a RRSet
func (h *HetznerProvider) deleteRRSet(ctx context.Context, rrset *hcloud.ZoneRRSet) error {
	_, _, err := h.client.Zone.DeleteRRSet(ctx, rrset)
	if err != nil {
		return fmt.Errorf("failed to delete RRSet: %w", err)
	}

	h.logger.Info("DNS record deleted successfully",
		zap.String("provider", "hetzner"),
		zap.String("rrset_id", rrset.ID),
	)

	return nil
}
