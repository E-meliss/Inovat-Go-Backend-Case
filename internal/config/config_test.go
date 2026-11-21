package config

import (
	"testing"
	"time"
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(lookupFrom(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.MaxDevicesPerOrg != 5 {
		t.Errorf("MaxDevicesPerOrg = %d, want 5", cfg.MaxDevicesPerOrg)
	}
	if cfg.HeartbeatTimeout != 60*time.Second {
		t.Errorf("HeartbeatTimeout = %v, want 60s", cfg.HeartbeatTimeout)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := load(lookupFrom(map[string]string{
		"DATABASE_URL":        "postgres://x",
		"MAX_DEVICES_PER_ORG": "12",
		"HEARTBEAT_TIMEOUT":   "90s",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.MaxDevicesPerOrg != 12 {
		t.Errorf("MaxDevicesPerOrg = %d, want 12", cfg.MaxDevicesPerOrg)
	}
	if cfg.HeartbeatTimeout != 90*time.Second {
		t.Errorf("HeartbeatTimeout = %v, want 90s", cfg.HeartbeatTimeout)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := map[string]map[string]string{
		"missing database url": {},
		"zero limit":           {"DATABASE_URL": "x", "MAX_DEVICES_PER_ORG": "0"},
		"non numeric limit":    {"DATABASE_URL": "x", "MAX_DEVICES_PER_ORG": "five"},
		"bad duration":         {"DATABASE_URL": "x", "HEARTBEAT_TIMEOUT": "soon"},
		"negative duration":    {"DATABASE_URL": "x", "WORKER_INTERVAL": "-1s"},
	}

	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := load(lookupFrom(env)); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}
