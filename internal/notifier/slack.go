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

// SlackNotifier sends notifications to a Slack webhook
type SlackNotifier struct {
	webhookURL string
	channel    string
	client     *http.Client
	logger     *zap.Logger
}

// NewSlackNotifier creates a new Slack notifier
func NewSlackNotifier(webhookURL, channel string, logger *zap.Logger) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		channel:    channel,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// slackMessage represents a Slack message payload
type slackMessage struct {
	Channel     string            `json:"channel,omitempty"`
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

type slackAttachment struct {
	Color  string       `json:"color"`
	Title  string       `json:"title"`
	Fields []slackField `json:"fields"`
	Footer string       `json:"footer"`
	Ts     int64        `json:"ts"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// Notify sends a failover notification to Slack
func (s *SlackNotifier) Notify(ctx context.Context, event interfaces.FailoverEvent) error {
	msg := slackMessage{
		Channel: s.channel,
		Text:    fmt.Sprintf(":rotating_light: IP Failover: %s -> %s", event.FromIP, event.ToIP),
		Attachments: []slackAttachment{
			{
				Color: "warning",
				Title: "IP Failover Event",
				Fields: []slackField{
					{Title: "From IP", Value: event.FromIP, Short: true},
					{Title: "To IP", Value: event.ToIP, Short: true},
					{Title: "Reason", Value: event.Reason, Short: false},
				},
				Footer: "ipfailover",
				Ts:     event.Timestamp.Unix(),
			},
		},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack request failed: %w", err)
	}
	defer resp.Body.Close()
	// Drain (bounded) so the keep-alive connection can be reused
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)); err != nil {
		s.logger.Debug("failed to drain slack response body", zap.Error(err))
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack returned HTTP %d", resp.StatusCode)
	}

	s.logger.Debug("slack notification sent",
		zap.String("channel", s.channel),
		zap.String("from_ip", event.FromIP),
		zap.String("to_ip", event.ToIP),
	)

	return nil
}

// Name returns the notifier name
func (s *SlackNotifier) Name() string {
	return "slack"
}
