package notifier

import (
	"context"
	"fmt"

	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

// MultiNotifier sends notifications to multiple notifiers
type MultiNotifier struct {
	notifiers []interfaces.Notifier
	logger    *zap.Logger
}

// NewMultiNotifier creates a notifier that dispatches to all given notifiers
func NewMultiNotifier(logger *zap.Logger, notifiers ...interfaces.Notifier) *MultiNotifier {
	return &MultiNotifier{
		notifiers: notifiers,
		logger:    logger,
	}
}

// Notify sends the event to all configured notifiers, collecting errors
func (m *MultiNotifier) Notify(ctx context.Context, event interfaces.FailoverEvent) error {
	var errs error
	for _, n := range m.notifiers {
		if err := n.Notify(ctx, event); err != nil {
			m.logger.Error("notification failed",
				zap.String("notifier", n.Name()),
				zap.Error(err),
			)
			errs = multierr.Append(errs, fmt.Errorf("%s: %w", n.Name(), err))
		}
	}
	return errs
}

// Name returns "multi"
func (m *MultiNotifier) Name() string {
	return "multi"
}

// NopNotifier is a no-op notifier for when notifications are not configured
type NopNotifier struct{}

// Notify does nothing
func (n *NopNotifier) Notify(ctx context.Context, event interfaces.FailoverEvent) error {
	return nil
}

// Name returns "nop"
func (n *NopNotifier) Name() string {
	return "nop"
}
