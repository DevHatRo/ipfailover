package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
)

// HetznerProvider implements DNSProvider for Hetzner DNS
type HetznerProvider struct {
	config *config.HetznerConfig
	client *http.Client
	logger *zap.Logger
}

// HetznerAPIResponse represents a Hetzner API response for list operations
type HetznerAPIResponse struct {
	Records []HetznerDNSRecord `json:"records"`
}

// HetznerSingleRecordResponse represents a Hetzner API response for single record operations
type HetznerSingleRecordResponse struct {
	Record HetznerDNSRecord `json:"record"`
}

// HetznerError represents a Hetzner API error
type HetznerError struct {
	Message string `json:"message"`
}

// HetznerDNSRecord represents a DNS record in Hetzner
type HetznerDNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	TTL     int    `json:"ttl"`
	ZoneID  string `json:"zone_id"`
	Created string `json:"created"`
	Modified string `json:"modified"`
}

// NewHetznerProvider creates a new Hetzner DNS provider
func NewHetznerProvider(cfg *config.HetznerConfig, logger *zap.Logger) *HetznerProvider {
	if cfg == nil {
		if logger != nil {
			logger.Error("hetzner config is nil")
		}
		return nil
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: false,
		},
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

	// First, try to find existing record
	existingRecord, err := h.findRecord(ctx, record.Name, record.Type)
	if err != nil {
		return errors.NewDNSProviderError("hetzner", record.Name, err)
	}

	if existingRecord != nil {
		// Update existing record
		return h.updateExistingRecord(ctx, existingRecord.ID, record)
	}

	// Create new record
	return h.createNewRecord(ctx, record)
}

// GetRecord retrieves an existing DNS record
func (h *HetznerProvider) GetRecord(ctx context.Context, name string, rtype string) (*interfaces.DNSRecord, error) {
	h.logger.Debug("getting DNS record",
		zap.String("provider", "hetzner"),
		zap.String("record", name),
		zap.String("type", rtype),
	)

	record, err := h.findRecord(ctx, name, rtype)
	if err != nil {
		return nil, errors.NewDNSProviderError("hetzner", name, err)
	}

	if record == nil {
		return nil, nil // Record not found
	}

	return &interfaces.DNSRecord{
		Name:     record.Name,
		Type:     record.Type,
		Value:    record.Value,
		TTL:      record.TTL,
		Provider: "hetzner",
		Metadata: map[string]string{
			"hetzner_id": record.ID,
			"zone_id":    record.ZoneID,
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

	record, err := h.findRecord(ctx, name, recordType)
	if err != nil {
		return errors.NewDNSProviderError("hetzner", name, err)
	}

	if record == nil {
		h.logger.Warn("record not found for deletion",
			zap.String("provider", "hetzner"),
			zap.String("record", name),
			zap.String("type", recordType),
		)
		return nil // Record doesn't exist, consider it deleted
	}

	if err := h.deleteRecordByID(ctx, record.ID); err != nil {
		return errors.NewDNSProviderError("hetzner", name, err)
	}

	return nil
}

// Validate checks if the provider configuration is valid
func (h *HetznerProvider) Validate(ctx context.Context) error {
	h.logger.Debug("validating Hetzner provider configuration")

	// Test API access by listing records
	_, err := h.listRecords(ctx)
	if err != nil {
		return fmt.Errorf("hetzner API validation failed: %w", err)
	}

	h.logger.Info("Hetzner provider validation successful")
	return nil
}

// findRecord finds a record by name and type
func (h *HetznerProvider) findRecord(ctx context.Context, name, recordType string) (*HetznerDNSRecord, error) {
	records, err := h.listRecords(ctx)
	if err != nil {
		return nil, err
	}

	for _, record := range records {
		if record.Name == name && (recordType == "" || record.Type == recordType) {
			return &record, nil
		}
	}

	return nil, nil // Record not found
}

// listRecords lists all DNS records for the zone
func (h *HetznerProvider) listRecords(ctx context.Context) ([]HetznerDNSRecord, error) {
	url := fmt.Sprintf("https://dns.hetzner.com/api/v1/records?zone_id=%s", h.config.ZoneID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Auth-API-Token", h.config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.NewHTTPError(resp.StatusCode, url, fmt.Errorf("unexpected status code"))
	}

	var apiResp HetznerAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return apiResp.Records, nil
}

// updateExistingRecord updates an existing DNS record
func (h *HetznerProvider) updateExistingRecord(ctx context.Context, recordID string, record interfaces.DNSRecord) error {
	url := fmt.Sprintf("https://dns.hetzner.com/api/v1/records/%s", recordID)

	updateData := HetznerDNSRecord{
		Type:   record.Type,
		Name:   record.Name,
		Value:  record.Value,
		TTL:    record.TTL,
		ZoneID: h.config.ZoneID,
	}

	jsonData, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("failed to marshal update data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Auth-API-Token", h.config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, url, fmt.Errorf("unexpected status code"))
	}

	var apiResp HetznerSingleRecordResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	h.logger.Info("DNS record updated successfully",
		zap.String("provider", "hetzner"),
		zap.String("record", record.Name),
		zap.String("record_id", recordID),
	)

	return nil
}

// createNewRecord creates a new DNS record
func (h *HetznerProvider) createNewRecord(ctx context.Context, record interfaces.DNSRecord) error {
	url := "https://dns.hetzner.com/api/v1/records"

	createData := HetznerDNSRecord{
		Type:   record.Type,
		Name:   record.Name,
		Value:  record.Value,
		TTL:    record.TTL,
		ZoneID: h.config.ZoneID,
	}

	jsonData, err := json.Marshal(createData)
	if err != nil {
		return fmt.Errorf("failed to marshal create data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Auth-API-Token", h.config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return errors.NewHTTPError(resp.StatusCode, url, fmt.Errorf("unexpected status code"))
	}

	var apiResp HetznerSingleRecordResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	h.logger.Info("DNS record created successfully",
		zap.String("provider", "hetzner"),
		zap.String("record", record.Name),
		zap.String("record_id", apiResp.Record.ID),
	)

	return nil
}

// deleteRecordByID deletes a DNS record by its ID
func (h *HetznerProvider) deleteRecordByID(ctx context.Context, recordID string) error {
	url := fmt.Sprintf("https://dns.hetzner.com/api/v1/records/%s", recordID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Auth-API-Token", h.config.APIToken)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, url, fmt.Errorf("unexpected status code"))
	}

	h.logger.Info("DNS record deleted successfully",
		zap.String("provider", "hetzner"),
		zap.String("record_id", recordID),
	)

	return nil
}
