package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/devhat/ipfailover/internal/app"
	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/internal/dns"
	"github.com/devhat/ipfailover/internal/ipchecker"
	"github.com/devhat/ipfailover/internal/metrics"
	"github.com/devhat/ipfailover/internal/notifier"
	"github.com/devhat/ipfailover/internal/state"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Build-time variables
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// getVersion returns the application version
func getVersion() string {
	return fmt.Sprintf("%s (built %s)", Version, BuildTime)
}

// createDNSProvider creates a DNS provider based on configuration using the registry
func createDNSProvider(dnsConfig config.DNSConfig, logger *zap.Logger) (interfaces.DNSProvider, error) {
	return dns.CreateProvider(&dnsConfig, logger)
}

// buildApplication wires together all dependencies and creates an Application
func buildApplication(cfg *config.Config, logger *zap.Logger) (*app.Application, error) {
	// Initialize DNS providers
	dnsProviders := make(map[string]interfaces.DNSProvider)
	for _, dnsConfig := range cfg.DNS {
		provider, err := createDNSProvider(dnsConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create DNS provider for %s: %w", dnsConfig.Name, err)
		}
		dnsProviders[dnsConfig.Name] = provider
	}

	ipCheck := ipchecker.NewHTTPChecker(cfg.CheckEndpoints, logger)
	stateStore := state.NewFileStateStore(cfg.StateFile, logger)
	metricsCollector := metrics.NewPrometheusCollector(logger)
	reachabilityChecker := app.NewTCPReachabilityChecker(
		cfg.ReachabilityPort,
		cfg.ReachabilityTimeout,
		logger,
	)

	// Build notifiers
	var notifiers []interfaces.Notifier
	if cfg.WebhookURL != "" {
		notifiers = append(notifiers, notifier.NewWebhookNotifier(cfg.WebhookURL, logger))
	}
	if cfg.SlackWebhookURL != "" {
		notifiers = append(notifiers, notifier.NewSlackNotifier(cfg.SlackWebhookURL, cfg.SlackChannel, logger))
	}

	var n interfaces.Notifier
	if len(notifiers) > 0 {
		n = notifier.NewMultiNotifier(logger, notifiers...)
	} else {
		n = &notifier.NopNotifier{}
	}

	return app.NewApplication(
		cfg,
		logger,
		ipCheck,
		dnsProviders,
		stateStore,
		metricsCollector,
		reachabilityChecker,
		n,
	), nil
}

func main() {
	var (
		configFile  = flag.String("config", "", "Path to configuration file")
		healthCheck = flag.Bool("health-check", false, "Perform health check and exit")
		version     = flag.Bool("version", false, "Show version information")
		help        = flag.Bool("help", false, "Show help information")
	)

	flag.Parse()

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

	if *version {
		fmt.Printf("IP Failover version: %s\n", getVersion())
		os.Exit(0)
	}

	if *healthCheck {
		if *configFile == "" {
			fmt.Fprintf(os.Stderr, "Error: -config flag is required for health check\n")
			os.Exit(1)
		}

		cfg, err := config.LoadConfig(*configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
			os.Exit(1)
		}

		logger, err := setupLogging(cfg.LogLevel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to setup logging: %v\n", err)
			os.Exit(1)
		}

		application, err := buildApplication(cfg, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create application: %v\n", err)
			os.Exit(1)
		}

		if err := application.HealthCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Health check passed")
		os.Exit(0)
	}

	if *configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -config flag is required\n")
		fmt.Fprintf(os.Stderr, "Use -help for usage information\n")
		os.Exit(1)
	}

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	logger, err := setupLogging(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to setup logging: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if syncErr := logger.Sync(); syncErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to sync logger: %v\n", syncErr)
		}
	}()

	logger.Info("IP failover daemon starting",
		zap.String("config", *configFile),
		zap.String("log_level", cfg.LogLevel),
	)

	application, err := buildApplication(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to create application", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT/SIGTERM for shutdown
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	// Handle SIGHUP for config reload
	reloadChan := make(chan os.Signal, 1)
	signal.Notify(reloadChan, syscall.SIGHUP)

	go func() {
		for {
			select {
			case sig := <-shutdownChan:
				logger.Info("Received shutdown signal",
					zap.String("signal", sig.String()),
				)
				cancel()
				return
			case <-reloadChan:
				logger.Info("Received SIGHUP, reloading configuration")
				newCfg, err := config.LoadConfig(*configFile)
				if err != nil {
					logger.Error("Failed to reload configuration, keeping current config",
						zap.Error(err),
					)
					continue
				}

				// Update mutable config fields on the running application
				application.ReloadConfig(newCfg)

				logger.Info("Configuration reloaded successfully",
					zap.String("primary_ip", newCfg.PrimaryIP),
					zap.String("secondary_ip", newCfg.SecondaryIP),
					zap.Duration("poll_interval", newCfg.PollInterval),
					zap.Int("failover_retries", newCfg.FailoverRetries),
				)
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := application.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatal("Application error", zap.Error(err))
	}

	logger.Info("Application shutdown complete")
}

// setupLogging configures logging based on the log level
func setupLogging(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()

	switch level {
	case "debug":
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		cfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return cfg.Build()
}
