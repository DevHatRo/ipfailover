package interfaces

import (
	"context"
	"time"
)

// DNSRecord represents a DNS record to be managed
type DNSRecord struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"` // A, AAAA, etc.
	Value    string            `json:"value"`
	TTL      int               `json:"ttl"`
	Provider string            `json:"provider"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// DNSProvider defines the interface for DNS operations
type DNSProvider interface {
	// Name returns the provider name (e.g., "cloudflare", "cpanel")
	Name() string

	// UpdateRecord updates or creates a DNS record
	UpdateRecord(ctx context.Context, record DNSRecord) error

	// GetRecord retrieves an existing DNS record
	GetRecord(ctx context.Context, name string, rtype string) (*DNSRecord, error)

	// DeleteRecord deletes a DNS record
	DeleteRecord(ctx context.Context, name, recordType string) error

	// Validate checks if the provider configuration is valid
	Validate(ctx context.Context) error
}

// IPChecker defines the interface for IP detection services
type IPChecker interface {
	// GetCurrentIP returns the current public IP address
	GetCurrentIP(ctx context.Context) (string, error)

	// Name returns the checker name (e.g., "ifconfig", "ipify")
	Name() string
}

// StateStore defines the interface for persisting application state
type StateStore interface {
	// GetLastAppliedIP returns the last IP that was successfully applied
	GetLastAppliedIP(ctx context.Context) (string, error)

	// SetLastAppliedIP stores the last applied IP
	SetLastAppliedIP(ctx context.Context, ip string) error

	// GetLastChangeTime returns the timestamp of the last IP change
	GetLastChangeTime(ctx context.Context) (time.Time, error)

	// SetLastChangeTime stores the timestamp of the last IP change
	SetLastChangeTime(ctx context.Context, t time.Time) error

	// SetLastCheckInfo stores information about the last IP check
	SetLastCheckInfo(ctx context.Context, ip string, t time.Time) error

	// GetLastCheckInfo returns information about the last IP check
	GetLastCheckInfo(ctx context.Context) (string, time.Time, error)

	// GetPrimaryFailureCount returns the current consecutive failure count for primary IP
	GetPrimaryFailureCount(ctx context.Context) (int, error)

	// SetPrimaryFailureCount sets the consecutive failure count for primary IP
	SetPrimaryFailureCount(ctx context.Context, count int) error

	// ResetPrimaryFailureCount resets the consecutive failure count for primary IP
	ResetPrimaryFailureCount(ctx context.Context) error
}

// MetricsCollector defines the interface for metrics collection
type MetricsCollector interface {
	// IncrementIPChecks increments the IP checks counter
	IncrementIPChecks()

	// IncrementIPCheckErrors increments the IP check errors counter
	IncrementIPCheckErrors()

	// IncrementDNSUpdates increments the DNS updates counter
	IncrementDNSUpdates(provider, record string)

	// IncrementDNSErrors increments the DNS update errors counter
	IncrementDNSErrors(provider, record string)

	// SetCurrentIP sets the current IP gauge
	SetCurrentIP(ip string)

	// SetLastChangeTime sets the last change timestamp
	SetLastChangeTime(t time.Time)

	// StartMetricsServer starts the metrics HTTP server
	StartMetricsServer(ctx context.Context, addr string) error
}
