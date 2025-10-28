package ipchecker_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devhat/ipfailover/internal/ipchecker"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestHTTPChecker_GetCurrentIP(t *testing.T) {
	tests := []struct {
		name           string
		mockResponse   string
		mockStatusCode int
		expectedIP     string
		expectedError  bool
	}{
		{
			name:           "successful response",
			mockResponse:   "203.0.113.10",
			mockStatusCode: 200,
			expectedIP:     "203.0.113.10",
			expectedError:  false,
		},
		{
			name:           "server error",
			mockResponse:   "",
			mockStatusCode: 500,
			expectedIP:     "",
			expectedError:  true,
		},
		{
			name:           "invalid IP response",
			mockResponse:   "invalid-ip",
			mockStatusCode: 200,
			expectedIP:     "",
			expectedError:  true,
		},
		{
			name:           "empty response",
			mockResponse:   "",
			mockStatusCode: 200,
			expectedIP:     "",
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.mockStatusCode)
				if _, err := w.Write([]byte(tt.mockResponse)); err != nil {
					t.Errorf("failed to write mock response: %v", err)
				}
			}))
			defer mockServer.Close()

			logger := zap.NewNop()
			checker := ipchecker.NewHTTPChecker([]string{mockServer.URL}, logger)

			// Act
			ip, err := checker.GetCurrentIP(context.Background())

			// Assert
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedIP, ip)
			}
		})
	}
}

func TestHTTPChecker_GetCurrentIP_MultipleEndpoints(t *testing.T) {
	// First server returns error
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		if _, err := w.Write([]byte("error")); err != nil {
			t.Errorf("failed to write error response: %v", err)
		}
	}))
	defer server1.Close()

	// Second server returns valid IP
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		if _, err := w.Write([]byte("203.0.113.10")); err != nil {
			t.Errorf("failed to write IP response: %v", err)
		}
	}))
	defer server2.Close()

	logger := zap.NewNop()
	checker := ipchecker.NewHTTPChecker([]string{server1.URL, server2.URL}, logger)

	ip, err := checker.GetCurrentIP(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "203.0.113.10", ip)
}

func TestHTTPChecker_GetCurrentIP_AllEndpointsFail(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server2.Close()

	logger := zap.NewNop()
	checker := ipchecker.NewHTTPChecker([]string{server1.URL, server2.URL}, logger)

	ip, err := checker.GetCurrentIP(context.Background())

	assert.Error(t, err)
	assert.Empty(t, ip)
}

func TestHTTPChecker_ValidateIP(t *testing.T) {
	tests := []struct {
		name        string
		ip          string
		expectError bool
	}{
		{
			name:        "valid IPv4",
			ip:          "203.0.113.10",
			expectError: false,
		},
		{
			name:        "valid IPv6",
			ip:          "2001:db8::1",
			expectError: false,
		},
		{
			name:        "invalid IP",
			ip:          "invalid-ip",
			expectError: true,
		},
		{
			name:        "empty IP",
			ip:          "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			checker := ipchecker.NewHTTPChecker([]string{}, logger)

			err := checker.ValidateIP(tt.ip)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHTTPChecker_Name(t *testing.T) {
	logger := zap.NewNop()
	checker := ipchecker.NewHTTPChecker([]string{}, logger)

	assert.Equal(t, "http", checker.Name())
}

func TestMockChecker(t *testing.T) {
	t.Run("successful response", func(t *testing.T) {
		mockChecker := ipchecker.NewMockChecker("203.0.113.10", nil)

		ip, err := mockChecker.GetCurrentIP(context.Background())

		assert.NoError(t, err)
		assert.Equal(t, "203.0.113.10", ip)
		assert.Equal(t, "mock", mockChecker.Name())
	})

	t.Run("error response", func(t *testing.T) {
		expectedErr := assert.AnError
		mockChecker := ipchecker.NewMockChecker("", expectedErr)

		ip, err := mockChecker.GetCurrentIP(context.Background())

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Empty(t, ip)
	})

	t.Run("set IP and error", func(t *testing.T) {
		mockChecker := ipchecker.NewMockChecker("", nil)

		mockChecker.SetIP("198.51.100.77")
		mockChecker.SetError(assert.AnError)

		ip, err := mockChecker.GetCurrentIP(context.Background())

		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
		assert.Equal(t, "198.51.100.77", ip)
	})
}

func TestHTTPChecker_ContextCancellation(t *testing.T) {
	// Create a server that takes a long time to respond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(200)
		if _, err := w.Write([]byte("203.0.113.10")); err != nil {
			t.Errorf("failed to write mock response: %v", err)
		}
	}))
	defer server.Close()

	logger := zap.NewNop()
	checker := ipchecker.NewHTTPChecker([]string{server.URL}, logger)

	// Create a context that cancels quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ip, err := checker.GetCurrentIP(ctx)

	assert.Error(t, err)
	assert.Empty(t, ip)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestHTTPChecker_UserAgent(t *testing.T) {
	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		w.WriteHeader(200)
		if _, err := w.Write([]byte("203.0.113.10")); err != nil {
			t.Errorf("failed to write mock response: %v", err)
		}
	}))
	defer server.Close()

	logger := zap.NewNop()
	checker := ipchecker.NewHTTPChecker([]string{server.URL}, logger)

	_, err := checker.GetCurrentIP(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "ipfailover/1.0", userAgent)
}
