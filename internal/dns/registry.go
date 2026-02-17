package dns

import (
	"fmt"

	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
)

// ProviderFactory creates a DNS provider from configuration
type ProviderFactory func(cfg *config.DNSConfig, logger *zap.Logger) (interfaces.DNSProvider, error)

var providerRegistry = map[string]ProviderFactory{
	"cloudflare": newCloudflareFromConfig,
	"cpanel":     newCPanelFromConfig,
	"route53":    newRoute53FromConfig,
	"hetzner":    newHetznerFromConfig,
}

// RegisteredProviders returns the names of all registered DNS providers
func RegisteredProviders() []string {
	names := make([]string, 0, len(providerRegistry))
	for name := range providerRegistry {
		names = append(names, name)
	}
	return names
}

// CreateProvider creates a DNS provider by name from the given configuration
func CreateProvider(dnsConfig *config.DNSConfig, logger *zap.Logger) (interfaces.DNSProvider, error) {
	factory, ok := providerRegistry[dnsConfig.Provider]
	if !ok {
		return nil, fmt.Errorf("unsupported DNS provider: %s", dnsConfig.Provider)
	}
	return factory(dnsConfig, logger)
}

// IsRegisteredProvider checks if a provider name is registered
func IsRegisteredProvider(name string) bool {
	_, ok := providerRegistry[name]
	return ok
}

func newCloudflareFromConfig(cfg *config.DNSConfig, logger *zap.Logger) (interfaces.DNSProvider, error) {
	if cfg.Cloudflare == nil {
		return nil, fmt.Errorf("cloudflare configuration is required")
	}
	p := NewCloudflareProvider(cfg.Cloudflare, logger)
	if p == nil {
		return nil, fmt.Errorf("failed to create cloudflare provider")
	}
	return p, nil
}

func newCPanelFromConfig(cfg *config.DNSConfig, logger *zap.Logger) (interfaces.DNSProvider, error) {
	if cfg.CPanel == nil {
		return nil, fmt.Errorf("cpanel configuration is required")
	}
	return NewCPanelProvider(cfg.CPanel, logger)
}

func newRoute53FromConfig(cfg *config.DNSConfig, logger *zap.Logger) (interfaces.DNSProvider, error) {
	if cfg.Route53 == nil {
		return nil, fmt.Errorf("route53 configuration is required")
	}
	return NewRoute53Provider(cfg.Route53, logger)
}

func newHetznerFromConfig(cfg *config.DNSConfig, logger *zap.Logger) (interfaces.DNSProvider, error) {
	if cfg.Hetzner == nil {
		return nil, fmt.Errorf("hetzner configuration is required")
	}
	p := NewHetznerProvider(cfg.Hetzner, logger)
	if p == nil {
		return nil, fmt.Errorf("failed to create hetzner provider")
	}
	return p, nil
}
