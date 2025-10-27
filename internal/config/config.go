package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

// Config represents the application configuration
type Config struct {
	// PollInterval is how often to check the IP address
	PollInterval time.Duration `mapstructure:"poll_interval"`

	// CheckEndpoints are the IP detection services to use
	CheckEndpoints []string `mapstructure:"check_endpoints"`

	// PrimaryIP is the primary IP address to use
	PrimaryIP string `mapstructure:"primary_ip"`

	// SecondaryIP is the secondary IP address to use
	SecondaryIP string `mapstructure:"secondary_ip"`

	// FailoverRetries is the number of consecutive failures before switching to secondary IP
	FailoverRetries int `mapstructure:"failover_retries"`

	// StateFailureStrategy defines how to handle state persistence failures
	// Options: "fail_fast", "continue_with_warning", "immediate_failover"
	StateFailureStrategy string `mapstructure:"state_failure_strategy"`

	// StateFile is the path to the state persistence file
	StateFile string `mapstructure:"state_file"`

	// MetricsAddr is the address for the metrics server
	MetricsAddr string `mapstructure:"metrics_addr"`

	// LogLevel is the logging level (debug, info, warn, error)
	LogLevel string `mapstructure:"log_level"`

	// DNS records to manage
	DNS []DNSConfig `mapstructure:"dns"`
}

// DNSConfig represents configuration for a DNS record
type DNSConfig struct {
	Name     string            `mapstructure:"name"`
	Type     string            `mapstructure:"type"`
	Provider string            `mapstructure:"provider"`
	TTL      int               `mapstructure:"ttl"`
	Metadata map[string]string `mapstructure:"metadata"`

	// Provider-specific configuration
	Cloudflare *CloudflareConfig `mapstructure:"cloudflare,omitempty"`
	CPanel     *CPanelConfig     `mapstructure:"cpanel,omitempty"`
	Route53    *Route53Config    `mapstructure:"route53,omitempty"`
	Namecheap  *NamecheapConfig  `mapstructure:"namecheap,omitempty"`
}

// CloudflareConfig represents Cloudflare-specific configuration
type CloudflareConfig struct {
	APIToken string `mapstructure:"api_token"`
	ZoneID   string `mapstructure:"zone_id"`
	Proxied  bool   `mapstructure:"proxied"`
}

// CPanelConfig represents cPanel-specific configuration
type CPanelConfig struct {
	BaseURL  string `mapstructure:"base_url"`
	Username string `mapstructure:"username"`
	APIToken string `mapstructure:"api_token"`
	Zone     string `mapstructure:"zone"`
}

// Route53Config represents Route53-specific configuration
type Route53Config struct {
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	Region          string `mapstructure:"region"`
	HostedZoneID    string `mapstructure:"hosted_zone_id"`
}

// NamecheapConfig represents Namecheap-specific configuration
type NamecheapConfig struct {
	APIUser  string `mapstructure:"api_user"`
	APIToken string `mapstructure:"api_token"`
	Username string `mapstructure:"username"`
	ClientIP string `mapstructure:"client_ip"`
	Domain   string `mapstructure:"domain"`
	Sandbox  bool   `mapstructure:"sandbox"`
}

// LoadConfig loads configuration from file and environment variables
func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// Set default values
	setDefaults()

	// Enable environment variable overrides
	viper.AutomaticEnv()

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// getDefaultStateFilePath returns a cross-platform default path for the state file
func getDefaultStateFilePath() string {
	// Try to use user config directory first (more appropriate for user applications)
	if configDir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(configDir, "ipfailover", "state.json")
	}

	// Fallback to temp directory if user config directory is not available
	return filepath.Join(os.TempDir(), "ipfailover", "state.json")
}

// setDefaults sets default configuration values
func setDefaults() {
	viper.SetDefault("poll_interval", "30s")
	viper.SetDefault("check_endpoints", []string{
		"https://ifconfig.io/ip",
		"https://api.ipify.org",
	})
	viper.SetDefault("failover_retries", 3)
	viper.SetDefault("state_failure_strategy", "continue_with_warning")
	viper.SetDefault("state_file", getDefaultStateFilePath())
	viper.SetDefault("metrics_addr", ":8080")
	viper.SetDefault("log_level", "info")
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.PollInterval <= 0 {
		return fmt.Errorf("poll_interval must be positive")
	}

	if len(c.CheckEndpoints) == 0 {
		return fmt.Errorf("at least one check_endpoint must be specified")
	}

	if c.PrimaryIP == "" {
		return fmt.Errorf("primary_ip must be specified")
	}

	if c.SecondaryIP == "" {
		return fmt.Errorf("secondary_ip must be specified")
	}

	if c.FailoverRetries < 0 {
		return fmt.Errorf("failover_retries must be non-negative")
	}

	// Validate state failure strategy
	validStrategies := map[string]bool{
		"fail_fast":             true,
		"continue_with_warning": true,
		"immediate_failover":    true,
	}
	if !validStrategies[c.StateFailureStrategy] {
		allowedValues := []string{"fail_fast", "continue_with_warning", "immediate_failover"}
		return fmt.Errorf("state_failure_strategy must be one of %v, got: %q", allowedValues, c.StateFailureStrategy)
	}

	if c.StateFile == "" {
		return fmt.Errorf("state_file must be specified")
	}

	if len(c.DNS) == 0 {
		return fmt.Errorf("at least one DNS record must be configured")
	}

	// Validate DNS records
	for i, dns := range c.DNS {
		if err := dns.Validate(); err != nil {
			return fmt.Errorf("DNS record %d validation failed: %w", i, err)
		}
	}

	return nil
}

// Validate validates a DNS configuration
func (d *DNSConfig) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("name is required")
	}

	if d.Type == "" {
		return fmt.Errorf("type is required")
	}

	if d.Provider == "" {
		return fmt.Errorf("provider is required")
	}

	if d.TTL <= 0 {
		return fmt.Errorf("TTL must be positive")
	}

	// Validate provider-specific configuration
	switch d.Provider {
	case "cloudflare":
		if d.Cloudflare == nil {
			return fmt.Errorf("cloudflare configuration is required for cloudflare provider")
		}
		if err := d.Cloudflare.Validate(); err != nil {
			return fmt.Errorf("cloudflare config validation failed: %w", err)
		}
	case "cpanel":
		if d.CPanel == nil {
			return fmt.Errorf("cpanel configuration is required for cpanel provider")
		}
		if err := d.CPanel.Validate(); err != nil {
			return fmt.Errorf("cpanel config validation failed: %w", err)
		}
	case "route53":
		if d.Route53 == nil {
			return fmt.Errorf("route53 configuration is required for route53 provider")
		}
		if err := d.Route53.Validate(); err != nil {
			return fmt.Errorf("route53 config validation failed: %w", err)
		}
	case "namecheap":
		if d.Namecheap == nil {
			return fmt.Errorf("namecheap configuration is required for namecheap provider")
		}
		if err := d.Namecheap.Validate(); err != nil {
			return fmt.Errorf("namecheap config validation failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported provider: %s", d.Provider)
	}

	return nil
}

// Validate validates Cloudflare configuration
func (c *CloudflareConfig) Validate() error {
	if c.APIToken == "" {
		return fmt.Errorf("api_token is required")
	}

	if c.ZoneID == "" {
		return fmt.Errorf("zone_id is required")
	}

	return nil
}

// Validate validates cPanel configuration
func (c *CPanelConfig) Validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}

	if c.Username == "" {
		return fmt.Errorf("username is required")
	}

	if c.APIToken == "" {
		return fmt.Errorf("api_token is required")
	}

	if c.Zone == "" {
		return fmt.Errorf("zone is required")
	}

	return nil
}

// Validate validates Route53 configuration
func (c *Route53Config) Validate() error {
	if c.AccessKeyID == "" {
		return fmt.Errorf("access_key_id is required")
	}

	if c.SecretAccessKey == "" {
		return fmt.Errorf("secret_access_key is required")
	}

	if c.Region == "" {
		return fmt.Errorf("region is required")
	}

	if c.HostedZoneID == "" {
		return fmt.Errorf("hosted_zone_id is required")
	}

	return nil
}

// Validate validates Namecheap configuration
func (c *NamecheapConfig) Validate() error {
	if c.APIUser == "" {
		return fmt.Errorf("api_user is required")
	}

	if c.APIToken == "" {
		return fmt.Errorf("api_token is required")
	}

	if c.Username == "" {
		return fmt.Errorf("username is required")
	}

	if c.ClientIP == "" {
		return fmt.Errorf("client_ip is required")
	}

	if c.Domain == "" {
		return fmt.Errorf("domain is required")
	}

	return nil
}

// String returns a safe string representation of CloudflareConfig with sensitive fields redacted
func (c *CloudflareConfig) String() string {
	return fmt.Sprintf("CloudflareConfig{APIToken:%s, ZoneID:%s, Proxied:%v}",
		"[REDACTED]", c.ZoneID, c.Proxied)
}

// String returns a safe string representation of CPanelConfig with sensitive fields redacted
func (c *CPanelConfig) String() string {
	return fmt.Sprintf("CPanelConfig{BaseURL:%s, Username:%s, APIToken:%s, Zone:%s}",
		c.BaseURL, c.Username, "[REDACTED]", c.Zone)
}

// String returns a safe string representation of Route53Config with sensitive fields redacted
func (c *Route53Config) String() string {
	return fmt.Sprintf("Route53Config{AccessKeyID:%s, SecretAccessKey:%s, Region:%s, HostedZoneID:%s}",
		"[REDACTED]", "[REDACTED]", c.Region, c.HostedZoneID)
}

// String returns a safe string representation of NamecheapConfig with sensitive fields redacted
func (c *NamecheapConfig) String() string {
	return fmt.Sprintf("NamecheapConfig{APIUser:%s, APIToken:%s, Username:%s, ClientIP:%s, Domain:%s, Sandbox:%v}",
		c.APIUser, "[REDACTED]", c.Username, "[REDACTED]", c.Domain, c.Sandbox)
}
