package app

import (
	"context"
	"fmt"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
)

// Application represents the main application
type Application struct {
	Config                *config.Config
	Logger                *zap.Logger
	IPChecker             interfaces.IPChecker
	DNSProviders          map[string]interfaces.DNSProvider
	StateStore            interfaces.StateStore
	Metrics               interfaces.MetricsCollector
	ReachabilityChecker   ReachabilityChecker
	Notifier              interfaces.Notifier
	TransientFailureCount int // In-memory fallback counter for when persistence fails
}

// ReachabilityChecker defines the interface for IP reachability checks
type ReachabilityChecker interface {
	CheckReachability(ctx context.Context, ip string) error
}

// NewApplication creates a new application instance from pre-built dependencies
func NewApplication(
	cfg *config.Config,
	logger *zap.Logger,
	ipChecker interfaces.IPChecker,
	dnsProviders map[string]interfaces.DNSProvider,
	stateStore interfaces.StateStore,
	metricsCollector interfaces.MetricsCollector,
	reachabilityChecker ReachabilityChecker,
	notifier interfaces.Notifier,
) *Application {
	return &Application{
		Config:              cfg,
		Logger:              logger,
		IPChecker:           ipChecker,
		DNSProviders:        dnsProviders,
		StateStore:          stateStore,
		Metrics:             metricsCollector,
		ReachabilityChecker: reachabilityChecker,
		Notifier:            notifier,
	}
}

// HealthCheck performs a health check and returns the status
func (app *Application) HealthCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := app.IPChecker.GetCurrentIP(ctx)
	if err != nil {
		return fmt.Errorf("IP check failed: %w", err)
	}

	_, err = app.StateStore.GetLastAppliedIP(ctx)
	if err != nil && !errors.IsNotFoundError(err) {
		return fmt.Errorf("state store check failed: %w", err)
	}

	return nil
}

// Run starts the application
func (app *Application) Run(ctx context.Context) error {
	app.Logger.Info("starting IP failover daemon")

	// Start metrics server
	metricsCtx, metricsCancel := context.WithCancel(ctx)
	defer metricsCancel()

	go func() {
		if err := app.Metrics.StartMetricsServer(metricsCtx, app.Config.MetricsAddr); err != nil {
			app.Logger.Error("metrics server error", zap.Error(err))
		}
	}()

	// Validate DNS providers
	for name, provider := range app.DNSProviders {
		if err := provider.Validate(ctx); err != nil {
			app.Logger.Error("DNS provider validation failed",
				zap.String("provider", name),
				zap.Error(err),
			)
			return fmt.Errorf("DNS provider %s validation failed: %w", name, err)
		}
		app.Logger.Info("DNS provider validated successfully",
			zap.String("provider", name),
		)
	}

	// Start main loop
	ticker := time.NewTicker(app.Config.PollInterval)
	defer ticker.Stop()

	// Run initial check
	if err := app.CheckAndUpdateIP(ctx); err != nil {
		app.Logger.Error("initial IP check failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			app.Logger.Info("shutting down application")
			return ctx.Err()
		case <-ticker.C:
			if err := app.CheckAndUpdateIP(ctx); err != nil {
				app.Logger.Error("IP check failed", zap.Error(err))
			}
		}
	}
}
