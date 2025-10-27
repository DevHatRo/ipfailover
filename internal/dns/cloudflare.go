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
	Success    bool                  `json:"success"`
	Errors     []CloudflareError     `json:"errors"`
	Result     []CloudflareDNSRecord `json:"result"`
	ResultInfo CloudflareResultInfo  `json:"result_info"`
}

// CloudflareResultInfo contains pagination information
type CloudflareResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
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
	if cfg == nil {
		if logger != nil {
			logger.Error("cloudflare config is nil")
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
// Uses efficient API filtering by both name and type
func (c *CloudflareProvider) GetRecord(ctx context.Context, name string, rtype string) (*interfaces.DNSRecord, error) {
	c.logger.Debug("getting DNS record",
		zap.String("provider", "cloudflare"),
		zap.String("record", name),
		zap.String("type", rtype),
	)

	records, err := c.getRecordsByNameAndType(ctx, name, rtype)
	if err != nil {
		return nil, errors.NewDNSProviderError("cloudflare", name, err)
	}

	if len(records) == 0 {
		return nil, nil // Record not found
	}

	// Since we filtered by type in the API call, we can return the first result
	record := records[0]
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

// DeleteRecord deletes a DNS record
func (c *CloudflareProvider) DeleteRecord(ctx context.Context, name, recordType string) error {
	c.logger.Info("deleting DNS record",
		zap.String("provider", "cloudflare"),
		zap.String("record", name),
		zap.String("type", recordType),
	)

	record, err := c.findRecord(ctx, name, recordType)
	if err != nil {
		return errors.NewDNSProviderError("cloudflare", name, err)
	}

	if record == nil {
		c.logger.Warn("record not found for deletion",
			zap.String("provider", "cloudflare"),
			zap.String("record", name),
			zap.String("type", recordType),
		)
		return nil // Record doesn't exist, consider it deleted
	}

	if err := c.deleteRecordByID(ctx, record.ID); err != nil {
		return errors.NewDNSProviderError("cloudflare", name, err)
	}

	return nil
}

// Validate checks if the provider configuration is valid
func (c *CloudflareProvider) Validate(ctx context.Context) error {
	c.logger.Debug("validating Cloudflare provider configuration")

	// Test API access by listing records
	_, err := c.listRecords(ctx)
	if err != nil {
		return fmt.Errorf("cloudflare API validation failed: %w", err)
	}

	c.logger.Info("Cloudflare provider validation successful")
	return nil
}

// findRecord finds a record by name and type
// Uses efficient API filtering when recordType is specified
func (c *CloudflareProvider) findRecord(ctx context.Context, name, recordType string) (*CloudflareDNSRecord, error) {
	var records []CloudflareDNSRecord
	var err error

	if recordType != "" {
		// Use efficient API filtering by both name and type
		records, err = c.getRecordsByNameAndType(ctx, name, recordType)
	} else {
		// Fall back to name-only filtering and check types in code
		records, err = c.getRecordsByName(ctx, name)
	}

	if err != nil {
		return nil, err
	}

	// If we used API type filtering, we can return the first result
	if recordType != "" && len(records) > 0 {
		return &records[0], nil
	}

	// Otherwise, filter by type in code (for backward compatibility)
	for _, record := range records {
		if recordType == "" || record.Type == recordType {
			return &record, nil
		}
	}

	return nil, nil // Record not found
}

// listRecords lists all DNS records for the zone with pagination support
func (c *CloudflareProvider) listRecords(ctx context.Context) ([]CloudflareDNSRecord, error) {
	allRecords := []CloudflareDNSRecord{}
	page := 1
	perPage := 100

	for {
		url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?page=%d&per_page=%d",
			c.config.ZoneID, page, perPage)

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
			return nil, fmt.Errorf("cloudflare API error: %v", apiResp.Errors)
		}

		allRecords = append(allRecords, apiResp.Result...)

		// Check if this is the last page
		if len(apiResp.Result) < perPage || page >= apiResp.ResultInfo.TotalPages {
			break
		}
		page++
	}

	return allRecords, nil
}

// getRecordsByName efficiently retrieves DNS records by name using API query parameters
func (c *CloudflareProvider) getRecordsByName(ctx context.Context, name string) ([]CloudflareDNSRecord, error) {
	return c.getRecordsByNameAndType(ctx, name, "")
}

// getRecordsByNameAndType retrieves DNS records filtered by name and optionally by type
func (c *CloudflareProvider) getRecordsByNameAndType(ctx context.Context, name, recordType string) ([]CloudflareDNSRecord, error) {
	allRecords := []CloudflareDNSRecord{}
	page := 1
	perPage := 100

	for {
		url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?name=%s&page=%d&per_page=%d",
			c.config.ZoneID, name, page, perPage)

		// Add type filter if specified
		if recordType != "" {
			url += fmt.Sprintf("&type=%s", recordType)
		}

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
			return nil, fmt.Errorf("cloudflare API error: %v", apiResp.Errors)
		}

		allRecords = append(allRecords, apiResp.Result...)

		// Check if this is the last page
		if len(apiResp.Result) < perPage || page >= apiResp.ResultInfo.TotalPages {
			break
		}
		page++
	}

	return allRecords, nil
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
		return fmt.Errorf("cloudflare API error: %v", apiResp.Errors)
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
		return fmt.Errorf("cloudflare API error: %v", apiResp.Errors)
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
		return fmt.Errorf("cloudflare API error: %v", apiResp.Errors)
	}

	c.logger.Info("DNS record deleted successfully",
		zap.String("provider", "cloudflare"),
		zap.String("record_id", recordID),
	)

	return nil
}
