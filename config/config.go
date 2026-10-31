// Package config reads the configuration every runner shares from the
// environment: where to serve, how to reach the Gateway and enrol, and how
// to log. A runner reads what is its own alongside it, with the same helpers.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultGRPCAddr              = ":50051"
	DefaultGatewayAddress        = "gateway:8080"
	DefaultZitiEnrollmentTimeout = 2 * time.Minute
	DefaultLogLevel              = "info"
)

// Config captures the runtime configuration every runner derives from the
// environment.
type Config struct {
	GRPCAddr              string
	ZitiEnabled           bool
	ServiceToken          string
	GatewayAddress        string
	ZitiEnrollmentTimeout time.Duration
	LogLevel              string
	// CatalogPath names the file the runner declares its catalog in. Empty
	// means none is declared.
	CatalogPath string
}

// Load reads configuration from environment variables, applying defaults when
// values are not provided. Returns an error when supplied values are invalid.
func Load() (Config, error) {
	var cfg Config

	cfg.GRPCAddr = ReadEnv("GRPC_ADDR", DefaultGRPCAddr)

	var err error
	cfg.ZitiEnabled, err = ReadBool("ZITI_ENABLED", false)
	if err != nil {
		return Config{}, err
	}

	if cfg.ZitiEnabled {
		cfg.GatewayAddress = ReadEnv("GATEWAY_ADDRESS", DefaultGatewayAddress)

		serviceToken := strings.TrimSpace(os.Getenv("SERVICE_TOKEN"))
		if serviceToken == "" {
			return Config{}, fmt.Errorf("SERVICE_TOKEN is required when ZITI_ENABLED is true")
		}
		cfg.ServiceToken = serviceToken

		enrollmentTimeout, err := ReadDuration("ZITI_ENROLLMENT_TIMEOUT", DefaultZitiEnrollmentTimeout)
		if err != nil {
			return Config{}, err
		}
		if enrollmentTimeout <= 0 {
			return Config{}, fmt.Errorf("ZITI_ENROLLMENT_TIMEOUT must be greater than 0")
		}
		cfg.ZitiEnrollmentTimeout = enrollmentTimeout
	} else {
		cfg.ZitiEnrollmentTimeout = DefaultZitiEnrollmentTimeout
	}

	cfg.CatalogPath = strings.TrimSpace(os.Getenv("CATALOG_PATH"))

	cfg.LogLevel = normalizeLogLevel(ReadEnv("LOG_LEVEL", DefaultLogLevel))

	return cfg, nil
}

// ReadEnv returns the trimmed value of key, or def when it is unset.
func ReadEnv(key, def string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return def
}

// ReadBool parses key as a boolean, or returns def when it is unset.
func ReadBool(key string, def bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return def, nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	parsed, err := strconv.ParseBool(trimmed)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

// ReadDuration parses key as a duration, or returns def when it is unset.
func ReadDuration(key string, def time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return def, nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("%s must be a duration", key)
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func normalizeLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return "info"
	case "debug":
		return "debug"
	case "warn", "warning":
		return "warn"
	case "error":
		return "error"
	default:
		return "info"
	}
}
