package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
)

// WebhookNotifier sends notifications via HTTP POST to a configurable URL
type WebhookNotifier struct {
	url    string
	client *http.Client
	logger *zap.Logger
}

// NewWebhookNotifier creates a new webhook notifier
func NewWebhookNotifier(url string, logger *zap.Logger) *WebhookNotifier {
	return &WebhookNotifier{
		url: url,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// webhookPayload is the JSON payload sent to the webhook URL
type webhookPayload struct {
	Event     string `json:"event"`
	FromIP    string `json:"from_ip"`
	ToIP      string `json:"to_ip"`
	Reason    string `json:"reason"`
	Timestamp string `json:"timestamp"`
}

// Notify sends a failover notification via HTTP POST
func (w *WebhookNotifier) Notify(ctx context.Context, event interfaces.FailoverEvent) error {
	payload := webhookPayload{
		Event:     "ip_failover",
		FromIP:    event.FromIP,
		ToIP:      event.ToIP,
		Reason:    event.Reason,
		Timestamp: event.Timestamp.UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	// Drain (bounded) so the keep-alive connection can be reused
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)); err != nil {
		w.logger.Debug("failed to drain webhook response body", zap.Error(err))
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}

	w.logger.Debug("webhook notification sent",
		zap.String("url", w.url),
		zap.String("from_ip", event.FromIP),
		zap.String("to_ip", event.ToIP),
	)

	return nil
}

// Name returns the notifier name
func (w *WebhookNotifier) Name() string {
	return "webhook"
}
