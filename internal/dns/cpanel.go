package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
)

// CPanelProvider implements DNSProvider for cPanel
type CPanelProvider struct {
	config *config.CPanelConfig
	client *http.Client
	logger *zap.Logger
}

// CPanelAPIResponse represents a cPanel API response
type CPanelAPIResponse struct {
	Result struct {
		Data []CPanelDNSRecord `json:"data"`
		Meta struct {
			Result int `json:"result"`
		} `json:"meta"`
	} `json:"result"`
}

// CPanelDNSRecord represents a DNS record in cPanel
type CPanelDNSRecord struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	Record string `json:"record"`
	Data   string `json:"data"`
	TTL    int    `json:"ttl"`
	Serial int    `json:"serial"`
	Line   int    `json:"line"`
}

// NewCPanelProvider creates a new cPanel DNS provider
func NewCPanelProvider(cfg *config.CPanelConfig, logger *zap.Logger) *CPanelProvider {
	if cfg == nil {
		panic("NewCPanelProvider: cfg must not be nil")
	}
	if logger == nil {
		panic("NewCPanelProvider: logger must not be nil")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: true,
		},
	}

	return &CPanelProvider{
		config: cfg,
		client: client,
		logger: logger,
	}
}

// Name returns the provider name
func (c *CPanelProvider) Name() string {
	return "cpanel"
}

// UpdateRecord updates or creates a DNS record
func (c *CPanelProvider) UpdateRecord(ctx context.Context, record interfaces.DNSRecord) error {
	c.logger.Info("updating DNS record",
		zap.String("provider", "cpanel"),
		zap.String("record", record.Name),
		zap.String("type", record.Type),
		zap.String("value", record.Value),
	)

	// First, try to find existing record
	existingRecord, err := c.findRecord(ctx, record.Name, record.Type)
	if err != nil {
		return errors.NewDNSProviderError("cpanel", record.Name, err)
	}

	if existingRecord != nil {
		// Update existing record
		return c.updateExistingRecord(ctx, existingRecord.Line, record)
	}

	// Create new record
	return c.createNewRecord(ctx, record)
}

// GetRecord retrieves an existing DNS record
func (c *CPanelProvider) GetRecord(ctx context.Context, name string, rtype string) (*interfaces.DNSRecord, error) {
	c.logger.Debug("getting DNS record",
		zap.String("provider", "cpanel"),
		zap.String("record", name),
		zap.String("type", rtype),
	)

	records, err := c.listRecords(ctx)
	if err != nil {
		return nil, errors.NewDNSProviderError("cpanel", name, err)
	}

	for _, record := range records {
		if record.Name == name && record.Type == rtype {
			return &interfaces.DNSRecord{
				Name:     record.Name,
				Type:     record.Type,
				Value:    record.Data,
				TTL:      record.TTL,
				Provider: "cpanel",
				Metadata: map[string]string{
					"cpanel_id": record.ID,
					"line":      fmt.Sprintf("%d", record.Line),
				},
			}, nil
		}
	}

	return nil, nil // Record not found
}

// DeleteRecord deletes a DNS record
func (c *CPanelProvider) DeleteRecord(ctx context.Context, name, recordType string) error {
	c.logger.Info("deleting DNS record",
		zap.String("provider", "cpanel"),
		zap.String("record", name),
		zap.String("type", recordType),
	)

	record, err := c.findRecord(ctx, name, recordType)
	if err != nil {
		return errors.NewDNSProviderError("cpanel", name, err)
	}

	if record == nil {
		c.logger.Warn("record not found for deletion",
			zap.String("provider", "cpanel"),
			zap.String("record", name),
			zap.String("type", recordType),
		)
		return nil // Record doesn't exist, consider it deleted
	}

	if err := c.deleteRecordByLine(ctx, record.Line); err != nil {
		return errors.NewDNSProviderError("cpanel", name, err)
	}

	return nil
}

// Validate checks if the provider configuration is valid
func (c *CPanelProvider) Validate(ctx context.Context) error {
	c.logger.Debug("validating cPanel provider configuration")

	// Test API access by listing records
	_, err := c.listRecords(ctx)
	if err != nil {
		return fmt.Errorf("cPanel API validation failed: %w", err)
	}

	c.logger.Info("cPanel provider validation successful")
	return nil
}

// findRecord finds a record by name and type
func (c *CPanelProvider) findRecord(ctx context.Context, name, recordType string) (*CPanelDNSRecord, error) {
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
func (c *CPanelProvider) listRecords(ctx context.Context) ([]CPanelDNSRecord, error) {
	apiURL := fmt.Sprintf("%s/execute/DnsLookup/get_dns_records", c.config.BaseURL)

	params := url.Values{}
	params.Set("domain", c.config.Zone)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.config.Username, c.config.APIToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.NewHTTPError(resp.StatusCode, apiURL, fmt.Errorf("unexpected status code"))
	}

	var apiResp CPanelAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if apiResp.Result.Meta.Result != 1 {
		return nil, fmt.Errorf("cPanel API error: result code %d", apiResp.Result.Meta.Result)
	}

	return apiResp.Result.Data, nil
}

// updateExistingRecord updates an existing DNS record
func (c *CPanelProvider) updateExistingRecord(ctx context.Context, line int, record interfaces.DNSRecord) error {
	apiURL := fmt.Sprintf("%s/execute/DnsLookup/update_dns_record", c.config.BaseURL)

	updateData := map[string]interface{}{
		"domain": c.config.Zone,
		"line":   line,
		"type":   record.Type,
		"name":   record.Name,
		"data":   record.Value,
		"ttl":    record.TTL,
	}

	jsonData, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("failed to marshal update data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.config.Username, c.config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, apiURL, fmt.Errorf("unexpected status code"))
	}

	var apiResp CPanelAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if apiResp.Result.Meta.Result != 1 {
		return fmt.Errorf("cPanel API error: result code %d", apiResp.Result.Meta.Result)
	}

	c.logger.Info("DNS record updated successfully",
		zap.String("provider", "cpanel"),
		zap.String("record", record.Name),
		zap.Int("line", line),
	)

	return nil
}

// createNewRecord creates a new DNS record
func (c *CPanelProvider) createNewRecord(ctx context.Context, record interfaces.DNSRecord) error {
	apiURL := fmt.Sprintf("%s/execute/DnsLookup/add_dns_record", c.config.BaseURL)

	createData := map[string]interface{}{
		"domain": c.config.Zone,
		"type":   record.Type,
		"name":   record.Name,
		"data":   record.Value,
		"ttl":    record.TTL,
	}

	jsonData, err := json.Marshal(createData)
	if err != nil {
		return fmt.Errorf("failed to marshal create data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.config.Username, c.config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, apiURL, fmt.Errorf("unexpected status code"))
	}

	var apiResp CPanelAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if apiResp.Result.Meta.Result != 1 {
		return fmt.Errorf("cPanel API error: result code %d", apiResp.Result.Meta.Result)
	}

	c.logger.Info("DNS record created successfully",
		zap.String("provider", "cpanel"),
		zap.String("record", record.Name),
	)

	return nil
}

// deleteRecordByLine deletes a DNS record by its line number
func (c *CPanelProvider) deleteRecordByLine(ctx context.Context, line int) error {
	apiURL := fmt.Sprintf("%s/execute/DnsLookup/delete_dns_record", c.config.BaseURL)

	deleteData := map[string]interface{}{
		"domain": c.config.Zone,
		"line":   line,
	}

	jsonData, err := json.Marshal(deleteData)
	if err != nil {
		return fmt.Errorf("failed to marshal delete data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.config.Username, c.config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, apiURL, fmt.Errorf("unexpected status code"))
	}

	var apiResp CPanelAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if apiResp.Result.Meta.Result != 1 {
		return fmt.Errorf("cPanel API error: result code %d", apiResp.Result.Meta.Result)
	}

	c.logger.Info("DNS record deleted successfully",
		zap.String("provider", "cpanel"),
		zap.Int("line", line),
	)

	return nil
}
