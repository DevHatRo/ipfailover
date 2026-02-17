package errors

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDNSProviderError(t *testing.T) {
	inner := fmt.Errorf("connection refused")
	err := NewDNSProviderError("cloudflare", "example.com", inner)

	assert.Equal(t, "DNS provider cloudflare failed for record example.com: connection refused", err.Error())
	assert.Equal(t, "cloudflare", err.Provider)
	assert.Equal(t, "example.com", err.Record)
	assert.ErrorIs(t, err, inner)
}

func TestIPCheckError(t *testing.T) {
	inner := fmt.Errorf("timeout")
	err := NewIPCheckError("ipify", inner)

	assert.Equal(t, "IP check service ipify failed: timeout", err.Error())
	assert.Equal(t, "ipify", err.Service)
	assert.ErrorIs(t, err, inner)
}

func TestConfigurationError(t *testing.T) {
	inner := fmt.Errorf("invalid value")
	err := NewConfigurationError("poll_interval", "-1s", inner)

	assert.Equal(t, "configuration error for field poll_interval (value: -1s): invalid value", err.Error())
	assert.Equal(t, "poll_interval", err.Field)
	assert.Equal(t, "-1s", err.Value)
	assert.ErrorIs(t, err, inner)
}

func TestStateError(t *testing.T) {
	inner := fmt.Errorf("disk full")
	err := NewStateError("save", inner)

	assert.Equal(t, "state operation save failed: disk full", err.Error())
	assert.Equal(t, "save", err.Operation)
	assert.ErrorIs(t, err, inner)
}

func TestHTTPError(t *testing.T) {
	inner := fmt.Errorf("server error")
	err := NewHTTPError(500, "https://api.example.com", inner)

	assert.Equal(t, "HTTP 500 error for https://api.example.com: server error", err.Error())
	assert.Equal(t, 500, err.StatusCode)
	assert.Equal(t, "https://api.example.com", err.URL)
	assert.ErrorIs(t, err, inner)
}

func TestNotFoundError(t *testing.T) {
	t.Run("with resource", func(t *testing.T) {
		inner := fmt.Errorf("no such file")
		err := NewNotFoundError("state file", inner)

		assert.Equal(t, "resource not found: state file: no such file", err.Error())
		assert.Equal(t, "state file", err.Resource)
		assert.ErrorIs(t, err, inner)
	})

	t.Run("without resource", func(t *testing.T) {
		inner := fmt.Errorf("missing")
		err := &NotFoundError{Err: inner}

		assert.Equal(t, "not found: missing", err.Error())
	})
}

func TestIsNotFoundError(t *testing.T) {
	t.Run("direct NotFoundError", func(t *testing.T) {
		err := NewNotFoundError("test", fmt.Errorf("inner"))
		assert.True(t, IsNotFoundError(err))
	})

	t.Run("wrapped NotFoundError", func(t *testing.T) {
		inner := NewNotFoundError("test", fmt.Errorf("inner"))
		wrapped := fmt.Errorf("outer: %w", inner)
		assert.True(t, IsNotFoundError(wrapped))
	})

	t.Run("non-NotFoundError", func(t *testing.T) {
		err := fmt.Errorf("some error")
		assert.False(t, IsNotFoundError(err))
	})

	t.Run("nil error", func(t *testing.T) {
		assert.False(t, IsNotFoundError(nil))
	})
}

func TestIsRetryableError(t *testing.T) {
	t.Run("HTTP 500 is retryable", func(t *testing.T) {
		err := NewHTTPError(500, "https://example.com", fmt.Errorf("server error"))
		assert.True(t, IsRetryableError(err))
	})

	t.Run("HTTP 429 is retryable", func(t *testing.T) {
		err := NewHTTPError(429, "https://example.com", fmt.Errorf("rate limited"))
		assert.True(t, IsRetryableError(err))
	})

	t.Run("HTTP 408 is retryable", func(t *testing.T) {
		err := NewHTTPError(408, "https://example.com", fmt.Errorf("timeout"))
		assert.True(t, IsRetryableError(err))
	})

	t.Run("HTTP 404 is not retryable", func(t *testing.T) {
		err := NewHTTPError(404, "https://example.com", fmt.Errorf("not found"))
		assert.False(t, IsRetryableError(err))
	})

	t.Run("HTTP 403 is not retryable", func(t *testing.T) {
		err := NewHTTPError(403, "https://example.com", fmt.Errorf("forbidden"))
		assert.False(t, IsRetryableError(err))
	})

	t.Run("DNSProviderError is retryable", func(t *testing.T) {
		err := NewDNSProviderError("cloudflare", "test.com", fmt.Errorf("timeout"))
		assert.True(t, IsRetryableError(err))
	})

	t.Run("IPCheckError is retryable", func(t *testing.T) {
		err := NewIPCheckError("ipify", fmt.Errorf("timeout"))
		assert.True(t, IsRetryableError(err))
	})

	t.Run("wrapped HTTP error is retryable", func(t *testing.T) {
		inner := NewHTTPError(502, "https://example.com", fmt.Errorf("bad gateway"))
		wrapped := fmt.Errorf("outer: %w", inner)
		assert.True(t, IsRetryableError(wrapped))
	})

	t.Run("plain error is not retryable", func(t *testing.T) {
		err := fmt.Errorf("some error")
		assert.False(t, IsRetryableError(err))
	})

	t.Run("StateError is not retryable", func(t *testing.T) {
		err := NewStateError("save", fmt.Errorf("disk full"))
		assert.False(t, IsRetryableError(err))
	})
}

func TestErrorUnwrapping(t *testing.T) {
	sentinel := fmt.Errorf("sentinel")

	t.Run("DNSProviderError unwraps", func(t *testing.T) {
		err := NewDNSProviderError("cf", "rec", sentinel)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("IPCheckError unwraps", func(t *testing.T) {
		err := NewIPCheckError("svc", sentinel)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("ConfigurationError unwraps", func(t *testing.T) {
		err := NewConfigurationError("field", "val", sentinel)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("StateError unwraps", func(t *testing.T) {
		err := NewStateError("op", sentinel)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("HTTPError unwraps", func(t *testing.T) {
		err := NewHTTPError(500, "url", sentinel)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("NotFoundError unwraps", func(t *testing.T) {
		err := NewNotFoundError("res", sentinel)
		require.ErrorIs(t, err, sentinel)
	})
}
