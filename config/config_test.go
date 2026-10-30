package config

import (
	"os"
	"testing"
)

func TestLoadZitiEnrollmentTimeoutDefault(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ZITI_ENABLED", "true")
	unsetEnv(t, "ZITI_ENROLLMENT_TIMEOUT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ZitiEnrollmentTimeout != DefaultZitiEnrollmentTimeout {
		t.Fatalf("expected ziti enrollment timeout %s, got %s", DefaultZitiEnrollmentTimeout, cfg.ZitiEnrollmentTimeout)
	}
}

func TestLoadZitiEnrollmentTimeoutInvalid(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ZITI_ENABLED", "true")
	t.Setenv("ZITI_ENROLLMENT_TIMEOUT", "0s")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid ziti enrollment timeout")
	}
}

func TestLoadServiceTokenRequiredWhenZitiEnabled(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ZITI_ENABLED", "true")
	unsetEnv(t, "SERVICE_TOKEN")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing SERVICE_TOKEN")
	}
}

func TestLoadGatewayAddressDefault(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ZITI_ENABLED", "true")
	unsetEnv(t, "GATEWAY_ADDRESS")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GatewayAddress != DefaultGatewayAddress {
		t.Fatalf("expected gateway address %s, got %s", DefaultGatewayAddress, cfg.GatewayAddress)
	}
}

func TestLoadGatewayAddressCustom(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ZITI_ENABLED", "true")
	t.Setenv("GATEWAY_ADDRESS", "gateway.internal:1234")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GatewayAddress != "gateway.internal:1234" {
		t.Fatalf("expected gateway address %s, got %s", "gateway.internal:1234", cfg.GatewayAddress)
	}
}

func TestLoadWithoutZitiNeedsNoToken(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ZITI_ENABLED", "false")
	unsetEnv(t, "SERVICE_TOKEN")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ZitiEnabled || cfg.ServiceToken != "" {
		t.Fatalf("expected ziti to be off, got %+v", cfg)
	}
	if cfg.ZitiEnrollmentTimeout != DefaultZitiEnrollmentTimeout {
		t.Fatalf("expected the default enrollment timeout, got %s", cfg.ZitiEnrollmentTimeout)
	}
}

func TestLoadRejectsMalformedZitiEnabled(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ZITI_ENABLED", "maybe")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for a malformed ZITI_ENABLED")
	}
}

func TestLoadReadsCatalogPathAndLogLevel(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("CATALOG_PATH", " /etc/agyn/catalog.yaml ")
	t.Setenv("LOG_LEVEL", "WARNING")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CatalogPath != "/etc/agyn/catalog.yaml" {
		t.Fatalf("expected the trimmed catalog path, got %q", cfg.CatalogPath)
	}
	if cfg.LogLevel != "warn" {
		t.Fatalf("expected warn, got %q", cfg.LogLevel)
	}
}

func TestLoadDefaultsAnUnknownLogLevelToInfo(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LOG_LEVEL", "loud")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected info, got %q", cfg.LogLevel)
	}
}

func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GRPC_ADDR", DefaultGRPCAddr)
	t.Setenv("LOG_LEVEL", DefaultLogLevel)
	t.Setenv("GATEWAY_ADDRESS", DefaultGatewayAddress)
	t.Setenv("SERVICE_TOKEN", "test-service-token")
	t.Setenv("CATALOG_PATH", "")
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	if ok {
		t.Cleanup(func() {
			_ = os.Setenv(key, value)
		})
	} else {
		t.Cleanup(func() {
			_ = os.Unsetenv(key)
		})
	}
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}
