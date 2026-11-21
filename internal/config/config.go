package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL      string
	HTTPAddr         string
	MaxDevicesPerOrg int
	HeartbeatTimeout time.Duration
	WorkerInterval   time.Duration
	ShutdownTimeout  time.Duration
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	var errs []error

	cfg := Config{
		DatabaseURL: stringVar(lookup, "DATABASE_URL", ""),
		HTTPAddr:    stringVar(lookup, "HTTP_ADDR", ":8080"),
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}

	var err error
	if cfg.MaxDevicesPerOrg, err = positiveInt(lookup, "MAX_DEVICES_PER_ORG", 5); err != nil {
		errs = append(errs, err)
	}
	if cfg.HeartbeatTimeout, err = positiveDuration(lookup, "HEARTBEAT_TIMEOUT", 60*time.Second); err != nil {
		errs = append(errs, err)
	}
	if cfg.WorkerInterval, err = positiveDuration(lookup, "WORKER_INTERVAL", 10*time.Second); err != nil {
		errs = append(errs, err)
	}
	if cfg.ShutdownTimeout, err = positiveDuration(lookup, "SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		errs = append(errs, err)
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func stringVar(lookup func(string) (string, bool), key, fallback string) string {
	if v, ok := lookup(key); ok && v != "" {
		return v
	}
	return fallback
}

func positiveInt(lookup func(string) (string, bool), key string, fallback int) (int, error) {
	raw := stringVar(lookup, key, "")
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", key, raw)
	}
	return n, nil
}

func positiveDuration(lookup func(string) (string, bool), key string, fallback time.Duration) (time.Duration, error) {
	raw := stringVar(lookup, key, "")
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration like 30s, got %q", key, raw)
	}
	return d, nil
}
