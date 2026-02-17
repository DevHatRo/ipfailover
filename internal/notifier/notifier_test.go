package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devhat/ipfailover/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var testEvent = interfaces.FailoverEvent{
	FromIP:    "10.0.0.1",
	ToIP:      "10.0.0.2",
	Reason:    "primary unreachable",
	Timestamp: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
}

func TestWebhookNotifier_Success(t *testing.T) {
	var received webhookPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		err := json.NewDecoder(r.Body).Decode(&received)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewWebhookNotifier(server.URL, zap.NewNop())
	err := n.Notify(context.Background(), testEvent)
	require.NoError(t, err)

	assert.Equal(t, "ip_failover", received.Event)
	assert.Equal(t, "10.0.0.1", received.FromIP)
	assert.Equal(t, "10.0.0.2", received.ToIP)
	assert.Equal(t, "primary unreachable", received.Reason)
}

func TestWebhookNotifier_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	n := NewWebhookNotifier(server.URL, zap.NewNop())
	err := n.Notify(context.Background(), testEvent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestWebhookNotifier_Name(t *testing.T) {
	n := NewWebhookNotifier("http://example.com", zap.NewNop())
	assert.Equal(t, "webhook", n.Name())
}

func TestSlackNotifier_Success(t *testing.T) {
	var received slackMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		err := json.NewDecoder(r.Body).Decode(&received)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewSlackNotifier(server.URL, "#alerts", zap.NewNop())
	err := n.Notify(context.Background(), testEvent)
	require.NoError(t, err)

	assert.Equal(t, "#alerts", received.Channel)
	assert.Contains(t, received.Text, "10.0.0.1")
	assert.Contains(t, received.Text, "10.0.0.2")
	require.Len(t, received.Attachments, 1)
	assert.Equal(t, "warning", received.Attachments[0].Color)
}

func TestSlackNotifier_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	n := NewSlackNotifier(server.URL, "#alerts", zap.NewNop())
	err := n.Notify(context.Background(), testEvent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
}

func TestSlackNotifier_Name(t *testing.T) {
	n := NewSlackNotifier("http://example.com", "#ch", zap.NewNop())
	assert.Equal(t, "slack", n.Name())
}

func TestMultiNotifier_AllSucceed(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewMultiNotifier(
		zap.NewNop(),
		NewWebhookNotifier(server.URL, zap.NewNop()),
		NewSlackNotifier(server.URL, "#ch", zap.NewNop()),
	)

	err := n.Notify(context.Background(), testEvent)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestMultiNotifier_PartialFailure(t *testing.T) {
	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer goodServer.Close()

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badServer.Close()

	n := NewMultiNotifier(
		zap.NewNop(),
		NewWebhookNotifier(goodServer.URL, zap.NewNop()),
		NewSlackNotifier(badServer.URL, "#ch", zap.NewNop()),
	)

	err := n.Notify(context.Background(), testEvent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slack")
}

func TestMultiNotifier_Name(t *testing.T) {
	n := NewMultiNotifier(zap.NewNop())
	assert.Equal(t, "multi", n.Name())
}

func TestNopNotifier(t *testing.T) {
	n := &NopNotifier{}
	err := n.Notify(context.Background(), testEvent)
	require.NoError(t, err)
	assert.Equal(t, "nop", n.Name())
}

func TestWebhookNotifier_CancelledContext(t *testing.T) {
	n := NewWebhookNotifier("http://example.com/webhook", zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := n.Notify(ctx, testEvent)
	require.Error(t, err)
}

func TestSlackNotifier_CancelledContext(t *testing.T) {
	n := NewSlackNotifier("http://example.com/slack", "#ch", zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := n.Notify(ctx, testEvent)
	require.Error(t, err)
}

func TestMultiNotifier_Empty(t *testing.T) {
	n := NewMultiNotifier(zap.NewNop())
	err := n.Notify(context.Background(), testEvent)
	require.NoError(t, err)
}

// Ensure implementations satisfy interface
var _ interfaces.Notifier = (*WebhookNotifier)(nil)
var _ interfaces.Notifier = (*SlackNotifier)(nil)
var _ interfaces.Notifier = (*MultiNotifier)(nil)
var _ interfaces.Notifier = (*NopNotifier)(nil)

// Test with invalid URL
func TestWebhookNotifier_InvalidURL(t *testing.T) {
	n := NewWebhookNotifier("://invalid", zap.NewNop())
	err := n.Notify(context.Background(), testEvent)
	require.Error(t, err)
}

func TestSlackNotifier_InvalidURL(t *testing.T) {
	n := NewSlackNotifier("://invalid", "#ch", zap.NewNop())
	err := n.Notify(context.Background(), testEvent)
	require.Error(t, err)
}

type failingNotifier struct{}

func (f *failingNotifier) Notify(ctx context.Context, event interfaces.FailoverEvent) error {
	return fmt.Errorf("always fails")
}
func (f *failingNotifier) Name() string { return "failing" }

func TestMultiNotifier_AllFail(t *testing.T) {
	n := NewMultiNotifier(
		zap.NewNop(),
		&failingNotifier{},
		&failingNotifier{},
	)

	err := n.Notify(context.Background(), testEvent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failing")
}
