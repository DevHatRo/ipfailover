package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
)

// Application represents the main application
type Application struct {
	// Config is guarded by configMu: read it via Cfg() and replace it via
	// ReloadConfig() once the daemon is running.
	Config                *config.Config
	Logger                *zap.Logger
	IPChecker             interfaces.IPChecker
	DNSProviders          map[string]interfaces.DNSProvider
	StateStore            interfaces.StateStore
	Metrics               interfaces.MetricsCollector
	ReachabilityChecker   ReachabilityChecker
	Notifier              interfaces.Notifier
	TransientFailureCount int // In-memory fallback counter for when persistence fails

	configMu sync.RWMutex
	reloadCh chan struct{}
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
		reloadCh:            make(chan struct{}, 1),
	}
}

// Cfg returns the current configuration. Callers must take a single snapshot
// per operation instead of reading app.Config directly, because ReloadConfig
// may swap the pointer concurrently.
func (app *Application) Cfg() *config.Config {
	app.configMu.RLock()
	defer app.configMu.RUnlock()
	return app.Config
}

// ReloadConfig atomically applies the runtime-mutable fields from newCfg and
// signals the run loop so a changed poll interval takes effect immediately.
func (app *Application) ReloadConfig(newCfg *config.Config) {
	app.configMu.Lock()
	updated := *app.Config
	updated.PollInterval = newCfg.PollInterval
	updated.FailoverRetries = newCfg.FailoverRetries
	updated.PrimaryIP = newCfg.PrimaryIP
	updated.SecondaryIP = newCfg.SecondaryIP
	updated.StateFailureStrategy = newCfg.StateFailureStrategy
	updated.ReachabilityPort = newCfg.ReachabilityPort
	updated.ReachabilityTimeout = newCfg.ReachabilityTimeout
	app.Config = &updated
	app.configMu.Unlock()

	if app.reloadCh != nil {
		select {
		case app.reloadCh <- struct{}{}:
		default:
		}
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
		if err := app.Metrics.StartMetricsServer(metricsCtx, app.Cfg().MetricsAddr); err != nil {
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
	pollInterval := app.Cfg().PollInterval
	ticker := time.NewTicker(pollInterval)
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
		case <-app.reloadCh:
			if newInterval := app.Cfg().PollInterval; newInterval != pollInterval {
				ticker.Reset(newInterval)
				app.Logger.Info("poll interval updated",
					zap.Duration("old_interval", pollInterval),
					zap.Duration("new_interval", newInterval),
				)
				pollInterval = newInterval
			}
		case <-ticker.C:
			if err := app.CheckAndUpdateIP(ctx); err != nil {
				app.Logger.Error("IP check failed", zap.Error(err))
			}
		}
	}
}
