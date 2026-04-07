// config.go — Application configuration loaded from environment variables.
package taskapi

import (
	"log"
	"os"
	"strconv"
)

// Config holds application settings.
type Config struct {
	Port         int
	DatabaseURL  string
	APIKeyHeader string
	LogLevel     string
	MaxPageSize  int
	CORSOrigins  []string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:         DefaultPort,
		DatabaseURL:  "sqlite://tasks.db",
		APIKeyHeader: "Authorization",
		LogLevel:     "info",
		MaxPageSize:  100,
		CORSOrigins:  []string{"*"},
	}
}

// LoadConfig reads configuration from environment variables,
// falling back to defaults for missing values.
func LoadConfig() *Config {
	cfg := DefaultConfig()

	if port := os.Getenv("PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}

	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		cfg.DatabaseURL = dbURL
	}

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		cfg.LogLevel = level
	}

	if maxPage := os.Getenv("MAX_PAGE_SIZE"); maxPage != "" {
		if m, err := strconv.Atoi(maxPage); err == nil && m > 0 {
			cfg.MaxPageSize = m
		}
	}

	log.Printf("Config loaded: port=%d db=%s log=%s", cfg.Port, cfg.DatabaseURL, cfg.LogLevel)
	return cfg
}

// Validate checks that required configuration is present.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return ErrInvalidPort
	}
	if c.DatabaseURL == "" {
		return ErrMissingDatabase
	}
	return nil
}

// Sentinel errors for configuration validation.
var (
	ErrInvalidPort    = &ConfigError{Field: "port", Message: "must be between 1 and 65535"}
	ErrMissingDatabase = &ConfigError{Field: "database_url", Message: "is required"}
)

// ConfigError represents a configuration validation failure.
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return e.Field + ": " + e.Message
}
