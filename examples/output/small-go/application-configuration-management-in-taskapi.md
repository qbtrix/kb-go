---
{
  "title": "Application Configuration Management in TaskAPI",
  "summary": "The config.go module provides environment-based configuration loading for the TaskAPI application. It defines a Config struct with sensible defaults, loads values from environment variables, and includes validation to ensure required settings are present and valid.",
  "concepts": [
    "environment variables",
    "configuration loading",
    "default values",
    "validation",
    "error handling",
    "Config struct",
    "ConfigError type",
    "sentinel errors"
  ],
  "categories": [
    "configuration management",
    "application initialization",
    "environment handling",
    "error types"
  ],
  "source_docs": [
    "c72e598bb6e7dcfc"
  ],
  "backlinks": null,
  "word_count": 288,
  "compiled_at": "2026-04-07T06:38:53Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Application Configuration Management

## Overview

The `config.go` module in the `taskapi` package handles all application configuration through environment variables with fallback defaults. This approach enables flexible deployment across different environments without code changes.

## Configuration Structure

### Config Type

The `Config` struct holds the following application settings:

- **Port** (int): HTTP server port for the API
- **DatabaseURL** (string): Connection string for the database
- **APIKeyHeader** (string): HTTP header name for API key authentication
- **LogLevel** (string): Logging verbosity level
- **MaxPageSize** (int): Maximum number of items per paginated response
- **CORSOrigins** ([]string): Allowed origins for CORS requests

## Configuration Loading

### Default Configuration

The `DefaultConfig()` function provides sensible defaults:

```
Port: DefaultPort constant
DatabaseURL: "sqlite://tasks.db"
APIKeyHeader: "Authorization"
LogLevel: "info"
MaxPageSize: 100
CORSOrigins: ["*"]
```

### Environment Variable Loading

The `LoadConfig()` function reads configuration from environment variables with the following mappings:

- `PORT`: Server port (must be valid integer)
- `DATABASE_URL`: Database connection string
- `LOG_LEVEL`: Logging level
- `MAX_PAGE_SIZE`: Maximum page size (must be positive integer)

Missing environment variables fall back to defaults. The function logs the loaded configuration for debugging purposes.

## Validation

The `Validate()` method ensures configuration validity:

- **Port**: Must be between 1 and 65535 (valid port range)
- **DatabaseURL**: Cannot be empty (required)

### Error Handling

Configuration errors are represented by the `ConfigError` type, which includes:

- **Field**: The configuration field that failed validation
- **Message**: Human-readable error description

Predefined sentinel errors:

- `ErrInvalidPort`: Port outside valid range
- `ErrMissingDatabase`: DatabaseURL not provided

## Usage Pattern

1. Call `LoadConfig()` to load environment variables
2. Call `Validate()` on the returned config
3. Use configuration values throughout the application

This ensures configuration is available early in application startup and invalid configurations are caught immediately.