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

// CloudflareProvider implements DNSProvider for Cloudflare
type CloudflareProvider struct {
	config *config.CloudflareConfig
	client *http.Client
	logger *zap.Logger
}

// CloudflareAPIResponse represents a Cloudflare API response
type CloudflareAPIResponse struct {
	Success bool                  `json:"success"`
	Errors  []CloudflareError     `json:"errors"`
	Result  []CloudflareDNSRecord `json:"result"`
}

// CloudflareError represents a Cloudflare API error
type CloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CloudflareDNSRecord represents a DNS record in Cloudflare
type CloudflareDNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// NewCloudflareProvider creates a new Cloudflare DNS provider
func NewCloudflareProvider(cfg *config.CloudflareConfig, logger *zap.Logger) *CloudflareProvider {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: true,
		},
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

// UpdateRecord updates or creates a DNS record
func (c *CloudflareProvider) UpdateRecord(ctx context.Context, record interfaces.DNSRecord) error {
	c.logger.Info("updating DNS record",
		zap.String("provider", "cloudflare"),
		zap.String("record", record.Name),
		zap.String("type", record.Type),
		zap.String("value", record.Value),
	)

	// First, try to find existing record
	existingRecord, err := c.findRecord(ctx, record.Name, record.Type)
	if err != nil {
		return errors.NewDNSProviderError("cloudflare", record.Name, err)
	}

	if existingRecord != nil {
		// Update existing record
		return c.updateExistingRecord(ctx, existingRecord.ID, record)
	}

	// Create new record
	return c.createNewRecord(ctx, record)
}

// GetRecord retrieves an existing DNS record
func (c *CloudflareProvider) GetRecord(ctx context.Context, name string) (*interfaces.DNSRecord, error) {
	c.logger.Debug("getting DNS record",
		zap.String("provider", "cloudflare"),
		zap.String("record", name),
	)

	records, err := c.listRecords(ctx)
	if err != nil {
		return nil, errors.NewDNSProviderError("cloudflare", name, err)
	}

	for _, record := range records {
		if record.Name == name {
			return &interfaces.DNSRecord{
				Name:     record.Name,
				Type:     record.Type,
				Value:    record.Content,
				TTL:      record.TTL,
				Provider: "cloudflare",
				Metadata: map[string]string{
					"cloudflare_id": record.ID,
					"proxied":       fmt.Sprintf("%t", record.Proxied),
				},
			}, nil
		}
	}

	return nil, nil // Record not found
}

// DeleteRecord deletes a DNS record
func (c *CloudflareProvider) DeleteRecord(ctx context.Context, name string) error {
	c.logger.Info("deleting DNS record",
		zap.String("provider", "cloudflare"),
		zap.String("record", name),
	)

	record, err := c.findRecord(ctx, name, "")
	if err != nil {
		return errors.NewDNSProviderError("cloudflare", name, err)
	}

	if record == nil {
		c.logger.Warn("record not found for deletion",
			zap.String("provider", "cloudflare"),
			zap.String("record", name),
		)
		return nil // Record doesn't exist, consider it deleted
	}

	return c.deleteRecordByID(ctx, record.ID)
}

// Validate checks if the provider configuration is valid
func (c *CloudflareProvider) Validate(ctx context.Context) error {
	c.logger.Debug("validating Cloudflare provider configuration")

	// Test API access by listing records
	_, err := c.listRecords(ctx)
	if err != nil {
		return fmt.Errorf("Cloudflare API validation failed: %w", err)
	}

	c.logger.Info("Cloudflare provider validation successful")
	return nil
}

// findRecord finds a record by name and type
func (c *CloudflareProvider) findRecord(ctx context.Context, name, recordType string) (*CloudflareDNSRecord, error) {
	records, err := c.listRecords(ctx)
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
func (c *CloudflareProvider) listRecords(ctx context.Context) ([]CloudflareDNSRecord, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", c.config.ZoneID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.NewHTTPError(resp.StatusCode, url, fmt.Errorf("unexpected status code"))
	}

	var apiResp CloudflareAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("Cloudflare API error: %v", apiResp.Errors)
	}

	return apiResp.Result, nil
}

// updateExistingRecord updates an existing DNS record
func (c *CloudflareProvider) updateExistingRecord(ctx context.Context, recordID string, record interfaces.DNSRecord) error {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", c.config.ZoneID, recordID)

	updateData := CloudflareDNSRecord{
		Type:    record.Type,
		Name:    record.Name,
		Content: record.Value,
		TTL:     record.TTL,
		Proxied: c.config.Proxied,
	}

	jsonData, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("failed to marshal update data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, url, fmt.Errorf("unexpected status code"))
	}

	var apiResp CloudflareAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("Cloudflare API error: %v", apiResp.Errors)
	}

	c.logger.Info("DNS record updated successfully",
		zap.String("provider", "cloudflare"),
		zap.String("record", record.Name),
		zap.String("record_id", recordID),
	)

	return nil
}

// createNewRecord creates a new DNS record
func (c *CloudflareProvider) createNewRecord(ctx context.Context, record interfaces.DNSRecord) error {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", c.config.ZoneID)

	createData := CloudflareDNSRecord{
		Type:    record.Type,
		Name:    record.Name,
		Content: record.Value,
		TTL:     record.TTL,
		Proxied: c.config.Proxied,
	}

	jsonData, err := json.Marshal(createData)
	if err != nil {
		return fmt.Errorf("failed to marshal create data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, url, fmt.Errorf("unexpected status code"))
	}

	var apiResp CloudflareAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("Cloudflare API error: %v", apiResp.Errors)
	}

	c.logger.Info("DNS record created successfully",
		zap.String("provider", "cloudflare"),
		zap.String("record", record.Name),
	)

	return nil
}

// deleteRecordByID deletes a DNS record by its ID
func (c *CloudflareProvider) deleteRecordByID(ctx context.Context, recordID string) error {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", c.config.ZoneID, recordID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.config.APIToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, url, fmt.Errorf("unexpected status code"))
	}

	var apiResp CloudflareAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("Cloudflare API error: %v", apiResp.Errors)
	}

	c.logger.Info("DNS record deleted successfully",
		zap.String("provider", "cloudflare"),
		zap.String("record_id", recordID),
	)

	return nil
}
