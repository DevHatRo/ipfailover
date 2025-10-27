package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/internal/dns"
	"github.com/devhat/ipfailover/internal/ipchecker"
	"github.com/devhat/ipfailover/internal/metrics"
	"github.com/devhat/ipfailover/internal/state"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Build-time variables
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// Application represents the main application
type Application struct {
	config       *config.Config
	logger       *zap.Logger
	ipChecker    interfaces.IPChecker
	dnsProviders map[string]interfaces.DNSProvider
	stateStore   interfaces.StateStore
	metrics      interfaces.MetricsCollector
}

// HealthCheck performs a health check and returns the status
func (app *Application) HealthCheck() error {
	// Check if we can get the current IP
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := app.ipChecker.GetCurrentIP(ctx)
	if err != nil {
		return fmt.Errorf("IP check failed: %w", err)
	}

	// Check if state store is accessible
	_, err = app.stateStore.GetLastAppliedIP(ctx)
	if err != nil && !errors.IsNotFoundError(err) {
		return fmt.Errorf("state store check failed: %w", err)
	}

	return nil
}

// NewApplication creates a new application instance
func NewApplication(cfg *config.Config, logger *zap.Logger) (*Application, error) {
	app := &Application{
		config:       cfg,
		logger:       logger,
		dnsProviders: make(map[string]interfaces.DNSProvider),
	}

	// Initialize IP checker
	app.ipChecker = ipchecker.NewHTTPChecker(cfg.CheckEndpoints, logger)

	// Initialize DNS providers
	for _, dnsConfig := range cfg.DNS {
		provider, err := app.createDNSProvider(dnsConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create DNS provider for %s: %w", dnsConfig.Name, err)
		}
		app.dnsProviders[dnsConfig.Name] = provider
	}

	// Initialize state store
	app.stateStore = state.NewFileStateStore(cfg.StateFile, logger)

	// Initialize metrics collector
	app.metrics = metrics.NewPrometheusCollector(logger)

	return app, nil
}

// createDNSProvider creates a DNS provider based on configuration
func (app *Application) createDNSProvider(dnsConfig config.DNSConfig) (interfaces.DNSProvider, error) {
	switch dnsConfig.Provider {
	case "cloudflare":
		if dnsConfig.Cloudflare == nil {
			return nil, fmt.Errorf("cloudflare configuration is required")
		}
		return dns.NewCloudflareProvider(dnsConfig.Cloudflare, app.logger), nil
	case "cpanel":
		if dnsConfig.CPanel == nil {
			return nil, fmt.Errorf("cpanel configuration is required")
		}
		return dns.NewCPanelProvider(dnsConfig.CPanel, app.logger), nil
	case "route53":
		if dnsConfig.Route53 == nil {
			return nil, fmt.Errorf("route53 configuration is required")
		}
		return dns.NewRoute53Provider(dnsConfig.Route53, app.logger)
	case "namecheap":
		if dnsConfig.Namecheap == nil {
			return nil, fmt.Errorf("namecheap configuration is required")
		}
		return dns.NewNamecheapProvider(dnsConfig.Namecheap, app.logger), nil
	default:
		return nil, fmt.Errorf("unsupported DNS provider: %s", dnsConfig.Provider)
	}
}

// Run starts the application
func (app *Application) Run(ctx context.Context) error {
	app.logger.Info("starting IP failover daemon")

	// Start metrics server
	metricsCtx, metricsCancel := context.WithCancel(ctx)
	defer metricsCancel()

	go func() {
		if err := app.metrics.StartMetricsServer(metricsCtx, app.config.MetricsAddr); err != nil {
			app.logger.Error("metrics server error", zap.Error(err))
		}
	}()

	// Validate DNS providers
	for name, provider := range app.dnsProviders {
		if err := provider.Validate(ctx); err != nil {
			app.logger.Error("DNS provider validation failed",
				zap.String("provider", name),
				zap.Error(err),
			)
			return fmt.Errorf("DNS provider %s validation failed: %w", name, err)
		}
		app.logger.Info("DNS provider validated successfully",
			zap.String("provider", name),
		)
	}

	// Start main loop
	ticker := time.NewTicker(app.config.PollInterval)
	defer ticker.Stop()

	// Run initial check
	if err := app.checkAndUpdateIP(ctx); err != nil {
		app.logger.Error("initial IP check failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			app.logger.Info("shutting down application")
			return ctx.Err()
		case <-ticker.C:
			if err := app.checkAndUpdateIP(ctx); err != nil {
				app.logger.Error("IP check failed", zap.Error(err))
			}
		}
	}
}

// checkAndUpdateIP checks the current IP and updates DNS records if needed
func (app *Application) checkAndUpdateIP(ctx context.Context) error {
	app.logger.Debug("checking current IP")
	app.metrics.IncrementIPChecks()

	// Get current IP
	currentIP, err := app.ipChecker.GetCurrentIP(ctx)
	if err != nil {
		app.metrics.IncrementIPCheckErrors()
		return errors.NewIPCheckError(app.ipChecker.Name(), err)
	}

	app.logger.Info("current IP detected",
		zap.String("ip", currentIP),
	)

	app.metrics.SetCurrentIP(currentIP)

	// Store check information
	if err := app.stateStore.SetLastCheckInfo(ctx, currentIP, time.Now()); err != nil {
		app.logger.Warn("failed to store check info", zap.Error(err))
	}

	// Determine target IP
	targetIP := app.determineTargetIP(currentIP)
	if targetIP == "" {
		app.logger.Debug("no target IP determined, skipping update")
		return nil
	}

	// Check if we need to update
	lastAppliedIP, err := app.stateStore.GetLastAppliedIP(ctx)
	if err != nil {
		app.logger.Warn("failed to get last applied IP", zap.Error(err))
	}

	if lastAppliedIP == targetIP {
		app.logger.Debug("IP already applied, skipping update",
			zap.String("ip", targetIP),
		)
		return nil
	}

	// Update DNS records
	if err := app.updateDNSRecords(ctx, targetIP); err != nil {
		return fmt.Errorf("failed to update DNS records: %w", err)
	}

	// Update state
	if err := app.stateStore.SetLastAppliedIP(ctx, targetIP); err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	app.metrics.SetLastChangeTime(time.Now())

	app.logger.Info("IP failover completed successfully",
		zap.String("from_ip", lastAppliedIP),
		zap.String("to_ip", targetIP),
	)

	return nil
}

// determineTargetIP determines which IP should be used based on active reachability check
// Implements retry logic: only switches to secondary after configurable number of consecutive failures
func (app *Application) determineTargetIP(currentIP string) string {
	// Create a context with a short timeout for reachability checks
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to reach the primary IP first
	err := app.checkIPReachability(ctx, app.config.PrimaryIP)
	if err == nil {
		// Primary is reachable, reset failure count and use primary
		if resetErr := app.stateStore.ResetPrimaryFailureCount(ctx); resetErr != nil {
			app.logger.Warn("failed to reset primary failure count", zap.Error(resetErr))
		}

		app.logger.Debug("Primary IP is reachable, using primary",
			zap.String("primary_ip", app.config.PrimaryIP),
		)
		return app.config.PrimaryIP
	}

	// Primary is unreachable, increment failure count
	failureCount, getErr := app.stateStore.GetPrimaryFailureCount(ctx)
	if getErr != nil {
		app.logger.Warn("failed to get primary failure count", zap.Error(getErr))
		failureCount = 0
	}

	failureCount++
	if setErr := app.stateStore.SetPrimaryFailureCount(ctx, failureCount); setErr != nil {
		app.logger.Warn("failed to set primary failure count", zap.Error(setErr))
	}

	app.logger.Debug("Primary IP unreachable, incrementing failure count",
		zap.String("primary_ip", app.config.PrimaryIP),
		zap.Int("failure_count", failureCount),
		zap.Int("max_retries", app.config.FailoverRetries),
		zap.Error(err),
	)

	// Check if we've exceeded the retry threshold
	if failureCount >= app.config.FailoverRetries {
		app.logger.Warn("Primary IP exceeded retry threshold, falling back to secondary",
			zap.String("primary_ip", app.config.PrimaryIP),
			zap.String("secondary_ip", app.config.SecondaryIP),
			zap.Int("failure_count", failureCount),
			zap.Int("max_retries", app.config.FailoverRetries),
		)
		return app.config.SecondaryIP
	}

	// Still within retry threshold, continue using primary
	app.logger.Debug("Primary IP still within retry threshold, continuing with primary",
		zap.String("primary_ip", app.config.PrimaryIP),
		zap.Int("failure_count", failureCount),
		zap.Int("max_retries", app.config.FailoverRetries),
	)
	return app.config.PrimaryIP
}

// checkIPReachability attempts to verify connectivity to the given IP address
func (app *Application) checkIPReachability(ctx context.Context, ip string) error {
	// Try to establish a TCP connection to a common port (80 for HTTP)
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "80"), 3*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s:80: %w", ip, err)
	}
	defer conn.Close()

	// Connection successful
	return nil
}

// updateDNSRecords updates all configured DNS records
func (app *Application) updateDNSRecords(ctx context.Context, targetIP string) error {
	var errs error

	for _, dnsConfig := range app.config.DNS {
		provider, exists := app.dnsProviders[dnsConfig.Name]
		if !exists {
			app.logger.Error("DNS provider not found",
				zap.String("record", dnsConfig.Name),
			)
			errs = multierr.Append(errs, fmt.Errorf("DNS provider not found for record %s", dnsConfig.Name))
			continue
		}

		record := interfaces.DNSRecord{
			Name:     dnsConfig.Name,
			Type:     dnsConfig.Type,
			Value:    targetIP,
			TTL:      dnsConfig.TTL,
			Provider: dnsConfig.Provider,
			Metadata: dnsConfig.Metadata,
		}

		if err := provider.UpdateRecord(ctx, record); err != nil {
			app.metrics.IncrementDNSErrors(dnsConfig.Provider, dnsConfig.Name)
			app.logger.Error("failed to update DNS record",
				zap.String("provider", dnsConfig.Provider),
				zap.String("record", dnsConfig.Name),
				zap.String("ip", targetIP),
				zap.Error(err),
			)
			errs = multierr.Append(errs, fmt.Errorf("failed to update DNS record %s with provider %s: %w", dnsConfig.Name, dnsConfig.Provider, err))
			continue
		}

		app.metrics.IncrementDNSUpdates(dnsConfig.Provider, dnsConfig.Name)
		app.logger.Info("DNS record updated successfully",
			zap.String("provider", dnsConfig.Provider),
			zap.String("record", dnsConfig.Name),
			zap.String("ip", targetIP),
		)
	}

	return errs
}

// getVersion returns the application version
func getVersion() string {
	return fmt.Sprintf("%s (built %s)", Version, BuildTime)
}

func main() {
	// Define command line flags
	var (
		configFile  = flag.String("config", "", "Path to configuration file")
		healthCheck = flag.Bool("health-check", false, "Perform health check and exit")
		version     = flag.Bool("version", false, "Show version information")
		help        = flag.Bool("help", false, "Show help information")
	)

	flag.Parse()

	// Handle help flag
	if *help {
		fmt.Printf("IP Failover - Automatic DNS failover service\n\n")
		fmt.Printf("Usage: %s [options]\n\n", os.Args[0])
		fmt.Printf("Options:\n")
		flag.PrintDefaults()
		fmt.Printf("\nExamples:\n")
		fmt.Printf("  %s -config /path/to/config.yaml\n", os.Args[0])
		fmt.Printf("  %s -health-check\n", os.Args[0])
		fmt.Printf("  %s -version\n", os.Args[0])
		os.Exit(0)
	}

	// Handle version flag
	if *version {
		fmt.Printf("IP Failover version: %s\n", getVersion())
		os.Exit(0)
	}

	// Handle health check flag
	if *healthCheck {
		if *configFile == "" {
			fmt.Fprintf(os.Stderr, "Error: -config flag is required for health check\n")
			os.Exit(1)
		}

		// Load minimal configuration for health check
		cfg, err := config.LoadConfig(*configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
			os.Exit(1)
		}

		// Setup minimal logging for health check
		logger, err := setupLogging(cfg.LogLevel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to setup logging: %v\n", err)
			os.Exit(1)
		}

		// Create application for health check
		app, err := NewApplication(cfg, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create application: %v\n", err)
			os.Exit(1)
		}

		// Perform health check
		if err := app.HealthCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Health check passed")
		os.Exit(0)
	}

	// Validate required config file
	if *configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -config flag is required\n")
		fmt.Fprintf(os.Stderr, "Use -help for usage information\n")
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Setup logging
	logger, err := setupLogging(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to setup logging: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("IP failover daemon starting",
		zap.String("config", *configFile),
		zap.String("log_level", cfg.LogLevel),
	)

	// Create application
	app, err := NewApplication(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to create application", zap.Error(err))
	}

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("Received signal, shutting down",
			zap.String("signal", sig.String()),
		)
		cancel()
	}()

	// Run application
	if err := app.Run(ctx); err != nil && err != context.Canceled {
		logger.Fatal("Application error", zap.Error(err))
	}

	logger.Info("Application shutdown complete")
}

// setupLogging configures logging based on the log level
func setupLogging(level string) (*zap.Logger, error) {
	config := zap.NewProductionConfig()

	switch level {
	case "debug":
		config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		config.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		config.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return config.Build()
}
