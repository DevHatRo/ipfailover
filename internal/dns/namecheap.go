package dns

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
)

// NamecheapProvider implements DNSProvider for Namecheap
type NamecheapProvider struct {
	config  *config.NamecheapConfig
	client  *http.Client
	logger  *zap.Logger
	baseURL string
}

// NamecheapAPIResponse represents a Namecheap API response
type NamecheapAPIResponse struct {
	CommandResponse struct {
		Type  string `xml:"Type,attr"`
		Error struct {
			Number string `xml:"Number,attr"`
			Text   string `xml:",chardata"`
		} `xml:"Errors>Error"`
		Data struct {
			Records []NamecheapDNSRecord `xml:"Record"`
		} `xml:"DomainDNSGetListResult"`
	} `xml:"ApiResponse"`
}

// NamecheapDNSRecord represents a DNS record in Namecheap
type NamecheapDNSRecord struct {
	ID      string `xml:"ID,attr"`
	Type    string `xml:"Type,attr"`
	Name    string `xml:"Name,attr"`
	Address string `xml:"Address,attr"`
	MXPref  string `xml:"MXPref,attr"`
	TTL     string `xml:"TTL,attr"`
}

// NewNamecheapProvider creates a new Namecheap DNS provider
func NewNamecheapProvider(cfg *config.NamecheapConfig, logger *zap.Logger) *NamecheapProvider {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: true,
		},
	}

	baseURL := "https://api.namecheap.com/xml.response"
	if cfg.Sandbox {
		baseURL = "https://api.sandbox.namecheap.com/xml.response"
	}

	return &NamecheapProvider{
		config:  cfg,
		client:  client,
		logger:  logger,
		baseURL: baseURL,
	}
}

// Name returns the provider name
func (n *NamecheapProvider) Name() string {
	return "namecheap"
}

// UpdateRecord updates or creates a DNS record
func (n *NamecheapProvider) UpdateRecord(ctx context.Context, record interfaces.DNSRecord) error {
	n.logger.Info("updating DNS record",
		zap.String("provider", "namecheap"),
		zap.String("record", record.Name),
		zap.String("type", record.Type),
		zap.String("value", record.Value),
	)

	// First, try to find existing record
	existingRecord, err := n.findRecord(ctx, record.Name, record.Type)
	if err != nil {
		return errors.NewDNSProviderError("namecheap", record.Name, err)
	}

	if existingRecord != nil {
		// Update existing record
		return n.updateExistingRecord(ctx, existingRecord.ID, record)
	}

	// Create new record
	return n.createNewRecord(ctx, record)
}

// GetRecord retrieves an existing DNS record
func (n *NamecheapProvider) GetRecord(ctx context.Context, name string, rtype string) (*interfaces.DNSRecord, error) {
	n.logger.Debug("getting DNS record",
		zap.String("provider", "namecheap"),
		zap.String("record", name),
		zap.String("type", rtype),
	)

	records, err := n.listRecords(ctx)
	if err != nil {
		return nil, errors.NewDNSProviderError("namecheap", name, err)
	}

	for _, record := range records {
		if record.Name == name && record.Type == rtype {
			ttl, err := strconv.Atoi(record.TTL)
			if err != nil {
				return nil, fmt.Errorf("parsing TTL for namecheap record %s: %w", record.ID, err)
			}
			return &interfaces.DNSRecord{
				Name:     record.Name,
				Type:     record.Type,
				Value:    record.Address,
				TTL:      ttl,
				Provider: "namecheap",
				Metadata: map[string]string{
					"namecheap_id": record.ID,
				},
			}, nil
		}
	}

	return nil, nil // Record not found
}

// DeleteRecord deletes a DNS record
func (n *NamecheapProvider) DeleteRecord(ctx context.Context, name, recordType string) error {
	n.logger.Info("deleting DNS record",
		zap.String("provider", "namecheap"),
		zap.String("record", name),
		zap.String("type", recordType),
	)

	record, err := n.findRecord(ctx, name, recordType)
	if err != nil {
		return errors.NewDNSProviderError("namecheap", name, err)
	}

	if record == nil {
		n.logger.Warn("record not found for deletion",
			zap.String("provider", "namecheap"),
			zap.String("record", name),
			zap.String("type", recordType),
		)
		return nil // Record doesn't exist, consider it deleted
	}

	if err := n.deleteRecordByID(ctx, record.ID); err != nil {
		return errors.NewDNSProviderError("namecheap", name, err)
	}

	return nil
}

// Validate checks if the provider configuration is valid
func (n *NamecheapProvider) Validate(ctx context.Context) error {
	n.logger.Debug("validating Namecheap provider configuration")

	// Test API access by listing records
	_, err := n.listRecords(ctx)
	if err != nil {
		return fmt.Errorf("api validation failed: %w", err)
	}

	n.logger.Info("Namecheap provider validation successful")
	return nil
}

// findRecord finds a record by name and type
func (n *NamecheapProvider) findRecord(ctx context.Context, name, recordType string) (*NamecheapDNSRecord, error) {
	records, err := n.listRecords(ctx)
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

// listRecords lists all DNS records for the domain
func (n *NamecheapProvider) listRecords(ctx context.Context) ([]NamecheapDNSRecord, error) {
	params := url.Values{}
	params.Set("ApiUser", n.config.APIUser)
	params.Set("ApiKey", n.config.APIToken)
	params.Set("UserName", n.config.Username)
	params.Set("Command", "namecheap.domains.dns.getList")
	params.Set("ClientIp", n.config.ClientIP)
	params.Set("Domain", n.config.Domain)

	req, err := http.NewRequestWithContext(ctx, "GET", n.baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.NewHTTPError(resp.StatusCode, n.baseURL, fmt.Errorf("unexpected status code"))
	}

	// Parse XML response
	var apiResp NamecheapAPIResponse
	decoder := xml.NewDecoder(resp.Body)
	if err := decoder.Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse XML response: %w", err)
	}

	// Check for API errors
	if apiResp.CommandResponse.Error.Number != "" {
		return nil, fmt.Errorf("api error %s: %s", apiResp.CommandResponse.Error.Number, apiResp.CommandResponse.Error.Text)
	}

	return apiResp.CommandResponse.Data.Records, nil
}

// performNamecheapPOSTRequest performs a POST request to Namecheap API and parses the XML response
func (n *NamecheapProvider) performNamecheapPOSTRequest(ctx context.Context, params url.Values, operation string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", n.baseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.URL.RawQuery = params.Encode()

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, n.baseURL, fmt.Errorf("unexpected status code"))
	}

	// Parse XML response to check for API-level errors
	var apiResp NamecheapAPIResponse
	decoder := xml.NewDecoder(resp.Body)
	if err := decoder.Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to parse XML response: %w", err)
	}

	// Check for API errors in the response
	if apiResp.CommandResponse.Error.Number != "" {
		return fmt.Errorf("api error %s: %s", apiResp.CommandResponse.Error.Number, apiResp.CommandResponse.Error.Text)
	}

	n.logger.Info(fmt.Sprintf("DNS record %s successfully", operation),
		zap.String("provider", "namecheap"),
	)

	return nil
}

// updateExistingRecord updates an existing DNS record
func (n *NamecheapProvider) updateExistingRecord(ctx context.Context, recordID string, record interfaces.DNSRecord) error {
	params := url.Values{}
	params.Set("ApiUser", n.config.APIUser)
	params.Set("ApiKey", n.config.APIToken)
	params.Set("UserName", n.config.Username)
	params.Set("Command", "namecheap.domains.dns.setHosts")
	params.Set("ClientIp", n.config.ClientIP)
	params.Set("Domain", n.config.Domain)
	params.Set("RecordId", recordID)
	params.Set("RecordType", record.Type)
	params.Set("HostName", record.Name)
	params.Set("Address", record.Value)
	params.Set("TTL", strconv.Itoa(record.TTL))

	err := n.performNamecheapPOSTRequest(ctx, params, "updated")
	if err != nil {
		return err
	}

	n.logger.Info("DNS record updated successfully",
		zap.String("provider", "namecheap"),
		zap.String("record", record.Name),
		zap.String("record_id", recordID),
	)

	return nil
}

// createNewRecord creates a new DNS record
func (n *NamecheapProvider) createNewRecord(ctx context.Context, record interfaces.DNSRecord) error {
	params := url.Values{}
	params.Set("ApiUser", n.config.APIUser)
	params.Set("ApiKey", n.config.APIToken)
	params.Set("UserName", n.config.Username)
	params.Set("Command", "namecheap.domains.dns.setHosts")
	params.Set("ClientIp", n.config.ClientIP)
	params.Set("Domain", n.config.Domain)
	params.Set("RecordType", record.Type)
	params.Set("HostName", record.Name)
	params.Set("Address", record.Value)
	params.Set("TTL", strconv.Itoa(record.TTL))

	err := n.performNamecheapPOSTRequest(ctx, params, "created")
	if err != nil {
		return err
	}

	n.logger.Info("DNS record created successfully",
		zap.String("provider", "namecheap"),
		zap.String("record", record.Name),
	)

	return nil
}

// deleteRecordByID deletes a DNS record by its ID
func (n *NamecheapProvider) deleteRecordByID(ctx context.Context, recordID string) error {
	params := url.Values{}
	params.Set("ApiUser", n.config.APIUser)
	params.Set("ApiKey", n.config.APIToken)
	params.Set("UserName", n.config.Username)
	params.Set("Command", "namecheap.domains.dns.delHost")
	params.Set("ClientIp", n.config.ClientIP)
	params.Set("Domain", n.config.Domain)
	params.Set("RecordId", recordID)

	req, err := http.NewRequestWithContext(ctx, "POST", n.baseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.URL.RawQuery = params.Encode()

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.NewHTTPError(resp.StatusCode, n.baseURL, fmt.Errorf("unexpected status code"))
	}

	// Read response body before closing
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse XML response
	var apiResp NamecheapAPIResponse
	if err := xml.Unmarshal(bodyBytes, &apiResp); err != nil {
		return fmt.Errorf("failed to parse XML response: %w", err)
	}

	// Check for API errors
	if apiResp.CommandResponse.Error.Number != "" {
		return errors.NewHTTPError(resp.StatusCode, n.baseURL, fmt.Errorf("api error %s: %s", apiResp.CommandResponse.Error.Number, apiResp.CommandResponse.Error.Text))
	}

	n.logger.Info("DNS record deleted successfully",
		zap.String("provider", "namecheap"),
		zap.String("record_id", recordID),
	)

	return nil
}
