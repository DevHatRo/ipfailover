package app

import (
	"context"
	"fmt"
	"time"

	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

// CheckAndUpdateIP checks the current IP and updates DNS records if needed
func (app *Application) CheckAndUpdateIP(ctx context.Context) error {
	app.Logger.Debug("checking current IP")
	app.Metrics.IncrementIPChecks()

	// Get current IP
	currentIP, err := app.IPChecker.GetCurrentIP(ctx)
	if err != nil {
		app.Metrics.IncrementIPCheckErrors()
		return errors.NewIPCheckError(app.IPChecker.Name(), err)
	}

	app.Logger.Info("current IP detected",
		zap.String("ip", currentIP),
	)

	app.Metrics.SetCurrentIP(currentIP)

	// Store check information
	if err := app.StateStore.SetLastCheckInfo(ctx, currentIP, time.Now()); err != nil {
		app.Logger.Warn("failed to store check info", zap.Error(err))
	}

	// Check if we need to update
	lastAppliedIP, err := app.StateStore.GetLastAppliedIP(ctx)
	if err != nil {
		app.Logger.Warn("failed to get last applied IP", zap.Error(err))
	}

	// Determine target IP
	targetIP, err := app.DetermineTargetIP(ctx, lastAppliedIP)
	if err != nil {
		return fmt.Errorf("failed to determine target IP: %w", err)
	}
	if targetIP == "" {
		app.Logger.Debug("no target IP determined, skipping update")
		return nil
	}

	if lastAppliedIP == targetIP {
		app.Logger.Debug("IP already applied, skipping update",
			zap.String("ip", targetIP),
		)
		return nil
	}

	// Update DNS records
	if err := app.UpdateDNSRecords(ctx, targetIP); err != nil {
		return fmt.Errorf("failed to update DNS records: %w", err)
	}

	// Update state
	if err := app.StateStore.SetLastAppliedIP(ctx, targetIP); err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	app.Metrics.SetLastChangeTime(time.Now())

	// Send notification about the failover
	if app.Notifier != nil {
		event := interfaces.FailoverEvent{
			FromIP:    lastAppliedIP,
			ToIP:      targetIP,
			Reason:    "IP failover",
			Timestamp: time.Now(),
		}
		if notifyErr := app.Notifier.Notify(ctx, event); notifyErr != nil {
			app.Logger.Error("failed to send failover notification", zap.Error(notifyErr))
		}
	}

	app.Logger.Info("IP failover completed successfully",
		zap.String("from_ip", lastAppliedIP),
		zap.String("to_ip", targetIP),
	)

	return nil
}

// DetermineTargetIP determines which IP should be used based on active reachability check.
// Implements retry logic: only switches to secondary after configurable number of consecutive failures.
// On first run (lastAppliedIP empty), verifies primary reachability before returning it.
func (app *Application) DetermineTargetIP(ctx context.Context, lastAppliedIP string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Try to reach the primary IP first
	err := app.ReachabilityChecker.CheckReachability(ctx, app.Config.PrimaryIP)
	if err == nil {
		// Primary is reachable, reset failure count and use primary
		if resetErr := app.StateStore.ResetPrimaryFailureCount(ctx); resetErr != nil {
			app.Logger.Error("critical: failed to reset primary failure count - state persistence compromised",
				zap.Error(resetErr),
				zap.String("primary_ip", app.Config.PrimaryIP),
				zap.Int("transient_failure_count", app.TransientFailureCount),
			)
			if app.Config.StateFailureStrategy == "fail_fast" {
				return "", fmt.Errorf("state persistence failure: failed to reset primary failure count: %w", resetErr)
			}
		} else {
			if app.TransientFailureCount > 0 {
				app.Logger.Info("primary IP recovered, resetting transient failure count",
					zap.String("primary_ip", app.Config.PrimaryIP),
					zap.Int("transient_failure_count", app.TransientFailureCount),
				)
				app.TransientFailureCount = 0
			}
		}

		app.Logger.Debug("Primary IP is reachable, using primary",
			zap.String("primary_ip", app.Config.PrimaryIP),
			zap.Int("transient_failure_count", app.TransientFailureCount),
		)
		return app.Config.PrimaryIP, nil
	}

	// Primary is unreachable, increment failure count
	failureCount, getErr := app.StateStore.GetPrimaryFailureCount(ctx)
	if getErr != nil {
		app.Logger.Error("critical: failed to get primary failure count - failover tracking compromised",
			zap.Error(getErr),
			zap.String("primary_ip", app.Config.PrimaryIP),
			zap.Int("transient_failure_count", app.TransientFailureCount),
		)

		switch app.Config.StateFailureStrategy {
		case "fail_fast":
			return "", fmt.Errorf("state persistence failure: failed to get primary failure count: %w", getErr)
		case "immediate_failover":
			app.Logger.Warn("state persistence failure - immediately failing over to secondary",
				zap.String("primary_ip", app.Config.PrimaryIP),
				zap.String("secondary_ip", app.Config.SecondaryIP),
				zap.Int("transient_failure_count", app.TransientFailureCount),
			)
			return app.Config.SecondaryIP, nil
		default:
			failureCount = 0
			app.Logger.Warn("using transient failure counter due to state persistence failure",
				zap.Int("transient_failure_count", app.TransientFailureCount),
				zap.Error(getErr),
			)
		}
	}

	failureCount++
	if setErr := app.StateStore.SetPrimaryFailureCount(ctx, failureCount); setErr != nil {
		app.TransientFailureCount++
		app.Logger.Error("critical: failed to persist primary failure count - using transient counter",
			zap.Error(setErr),
			zap.String("primary_ip", app.Config.PrimaryIP),
			zap.Int("failure_count", failureCount),
			zap.Int("transient_failure_count", app.TransientFailureCount),
		)

		switch app.Config.StateFailureStrategy {
		case "fail_fast":
			return "", fmt.Errorf("state persistence failure: failed to set primary failure count: %w", setErr)
		case "immediate_failover":
			app.Logger.Warn("state persistence failure - immediately failing over to secondary",
				zap.String("primary_ip", app.Config.PrimaryIP),
				zap.String("secondary_ip", app.Config.SecondaryIP),
				zap.Int("failure_count", failureCount),
				zap.Int("transient_failure_count", app.TransientFailureCount),
			)
			return app.Config.SecondaryIP, nil
		default:
			app.Logger.Warn("continuing with transient failure counter due to persistence failure",
				zap.Int("transient_failure_count", app.TransientFailureCount),
				zap.Error(setErr),
			)
		}
	} else {
		app.TransientFailureCount = 0
	}

	// If we have transient failures, attempt to persist them
	if app.TransientFailureCount > 0 {
		app.attemptTransientPersistence(ctx, failureCount)
	}

	totalFailureCount := failureCount + app.TransientFailureCount

	app.Logger.Debug("Primary IP unreachable, incrementing failure count",
		zap.String("primary_ip", app.Config.PrimaryIP),
		zap.Int("failure_count", failureCount),
		zap.Int("transient_failure_count", app.TransientFailureCount),
		zap.Int("total_failure_count", totalFailureCount),
		zap.Int("max_retries", app.Config.FailoverRetries),
		zap.Error(err),
	)

	// Check if we've exceeded the retry threshold
	if totalFailureCount >= app.Config.FailoverRetries {
		app.Logger.Warn("Primary IP exceeded retry threshold, falling back to secondary",
			zap.String("primary_ip", app.Config.PrimaryIP),
			zap.String("secondary_ip", app.Config.SecondaryIP),
			zap.Int("failure_count", failureCount),
			zap.Int("transient_failure_count", app.TransientFailureCount),
			zap.Int("total_failure_count", totalFailureCount),
			zap.Int("max_retries", app.Config.FailoverRetries),
		)
		return app.Config.SecondaryIP, nil
	}

	// First run: primary is unreachable, check secondary before using it
	if lastAppliedIP == "" {
		app.Logger.Error("First run detected with unreachable primary - checking secondary IP reachability",
			zap.String("primary_ip", app.Config.PrimaryIP),
			zap.String("secondary_ip", app.Config.SecondaryIP),
			zap.Int("failure_count", failureCount),
			zap.Int("max_retries", app.Config.FailoverRetries),
		)

		if secErr := app.ReachabilityChecker.CheckReachability(ctx, app.Config.SecondaryIP); secErr != nil {
			app.Logger.Error("Secondary IP is also unreachable - skipping DNS update",
				zap.String("primary_ip", app.Config.PrimaryIP),
				zap.String("secondary_ip", app.Config.SecondaryIP),
				zap.Error(secErr),
			)
			return "", nil
		}

		app.Logger.Info("Secondary IP is reachable - using secondary IP for DNS update",
			zap.String("primary_ip", app.Config.PrimaryIP),
			zap.String("secondary_ip", app.Config.SecondaryIP),
		)
		return app.Config.SecondaryIP, nil
	}

	// Not first run: still within retry threshold, continue using primary
	app.Logger.Debug("Primary IP still within retry threshold, continuing with primary",
		zap.String("primary_ip", app.Config.PrimaryIP),
		zap.Int("failure_count", failureCount),
		zap.Int("max_retries", app.Config.FailoverRetries),
	)
	return app.Config.PrimaryIP, nil
}

// UpdateDNSRecords updates all configured DNS records
func (app *Application) UpdateDNSRecords(ctx context.Context, targetIP string) error {
	var errs error

	for _, dnsConfig := range app.Config.DNS {
		provider, exists := app.DNSProviders[dnsConfig.Name]
		if !exists {
			app.Logger.Error("DNS provider not found",
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
			app.Metrics.IncrementDNSErrors(dnsConfig.Provider, dnsConfig.Name)
			app.Logger.Error("failed to update DNS record",
				zap.String("provider", dnsConfig.Provider),
				zap.String("record", dnsConfig.Name),
				zap.String("ip", targetIP),
				zap.Error(err),
			)
			errs = multierr.Append(errs, fmt.Errorf("failed to update DNS record %s with provider %s: %w", dnsConfig.Name, dnsConfig.Provider, err))
			continue
		}

		app.Metrics.IncrementDNSUpdates(dnsConfig.Provider, dnsConfig.Name)
		app.Logger.Info("DNS record updated successfully",
			zap.String("provider", dnsConfig.Provider),
			zap.String("record", dnsConfig.Name),
			zap.String("ip", targetIP),
		)
	}

	return errs
}

// attemptTransientPersistence attempts to persist transient failure count when possible
func (app *Application) attemptTransientPersistence(ctx context.Context, persistedCount int) {
	totalCount := persistedCount + app.TransientFailureCount

	if err := app.StateStore.SetPrimaryFailureCount(ctx, totalCount); err != nil {
		app.Logger.Debug("failed to persist transient failure count - will retry later",
			zap.Error(err),
			zap.Int("transient_failure_count", app.TransientFailureCount),
			zap.Int("total_count", totalCount),
		)
	} else {
		app.Logger.Info("successfully persisted transient failure count",
			zap.Int("transient_failure_count", app.TransientFailureCount),
			zap.Int("total_count", totalCount),
		)
		app.TransientFailureCount = 0
	}
}
