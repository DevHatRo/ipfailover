package errors

import (
	"fmt"
)

// Domain-specific error types for better error handling

// DNSProviderError represents errors from DNS providers
type DNSProviderError struct {
	Provider string
	Record   string
	Err      error
}

func (e *DNSProviderError) Error() string {
	return fmt.Sprintf("DNS provider %s failed for record %s: %v", e.Provider, e.Record, e.Err)
}

func (e *DNSProviderError) Unwrap() error {
	return e.Err
}

// IPCheckError represents errors from IP checking services
type IPCheckError struct {
	Service string
	Err     error
}

func (e *IPCheckError) Error() string {
	return fmt.Sprintf("IP check service %s failed: %v", e.Service, e.Err)
}

func (e *IPCheckError) Unwrap() error {
	return e.Err
}

// ConfigurationError represents configuration-related errors
type ConfigurationError struct {
	Field string
	Value interface{}
	Err   error
}

func (e *ConfigurationError) Error() string {
	return fmt.Sprintf("configuration error for field %s (value: %v): %v", e.Field, e.Value, e.Err)
}

func (e *ConfigurationError) Unwrap() error {
	return e.Err
}

// StateError represents state management errors
type StateError struct {
	Operation string
	Err       error
}

func (e *StateError) Error() string {
	return fmt.Sprintf("state operation %s failed: %v", e.Operation, e.Err)
}

func (e *StateError) Unwrap() error {
	return e.Err
}

// HTTPError represents HTTP-related errors with status code
type HTTPError struct {
	StatusCode int
	URL        string
	Err        error
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d error for %s: %v", e.StatusCode, e.URL, e.Err)
}

func (e *HTTPError) Unwrap() error {
	return e.Err
}

// IsRetryableError checks if an error is retryable
func IsRetryableError(err error) bool {
	switch e := err.(type) {
	case *HTTPError:
		// Retry on 5xx errors and some 4xx errors
		return e.StatusCode >= 500 || e.StatusCode == 429 || e.StatusCode == 408
	case *DNSProviderError:
		// DNS provider errors are generally retryable
		return true
	case *IPCheckError:
		// IP check errors are generally retryable
		return true
	default:
		return false
	}
}

// NewHTTPError creates a new HTTP error
func NewHTTPError(statusCode int, url string, err error) *HTTPError {
	return &HTTPError{
		StatusCode: statusCode,
		URL:        url,
		Err:        err,
	}
}

// NewDNSProviderError creates a new DNS provider error
func NewDNSProviderError(provider, record string, err error) *DNSProviderError {
	return &DNSProviderError{
		Provider: provider,
		Record:   record,
		Err:      err,
	}
}

// NewIPCheckError creates a new IP check error
func NewIPCheckError(service string, err error) *IPCheckError {
	return &IPCheckError{
		Service: service,
		Err:     err,
	}
}

// NewConfigurationError creates a new configuration error
func NewConfigurationError(field string, value interface{}, err error) *ConfigurationError {
	return &ConfigurationError{
		Field: field,
		Value: value,
		Err:   err,
	}
}

// NewStateError creates a new state error
func NewStateError(operation string, err error) *StateError {
	return &StateError{
		Operation: operation,
		Err:       err,
	}
}
