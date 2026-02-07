package config

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testProviderPrometheus = "prometheus"

func TestLoadFromDirMissingDirectory(t *testing.T) {
	cfg := NewOperatorConfig()
	if err := cfg.LoadFromDir(filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Fatalf("expected missing directory to be ignored, got error: %v", err)
	}
}

func TestLoadFromDirParsesValues(t *testing.T) {
	dir := t.TempDir()
	writeConfigKey(t, dir, "dry-run", "true")
	writeConfigKey(t, dir, "metrics-provider", testProviderPrometheus)
	writeConfigKey(t, dir, "prometheus-url", "http://prometheus-k8s.monitoring.svc:9090")
	writeConfigKey(t, dir, "reconciliation-interval", "2m")
	writeConfigKey(t, dir, "leader-election", "true")
	writeConfigKey(t, dir, "metrics-bind-address", ":8080")
	writeConfigKey(t, dir, "health-probe-bind-address", ":8081")
	writeConfigKey(t, dir, "metrics-server-min-samples-required", "15")
	writeConfigKey(t, dir, "metrics-server-sampling-interval", "30s")

	cfg := NewOperatorConfig()
	if err := cfg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir returned error: %v", err)
	}

	if !cfg.IsDryRun() {
		t.Fatalf("expected dry-run=true")
	}
	if got := cfg.GetMetricsProvider(); got != testProviderPrometheus {
		t.Fatalf("expected metrics provider prometheus, got %q", got)
	}
	if got := cfg.GetPrometheusURL(); got != "http://prometheus-k8s.monitoring.svc:9090" {
		t.Fatalf("unexpected prometheus URL: %q", got)
	}
	if got := cfg.GetReconciliationInterval(); got != 2*time.Minute {
		t.Fatalf("expected reconciliation-interval=2m, got %v", got)
	}
	if !cfg.IsLeaderElectionEnabled() {
		t.Fatalf("expected leader-election=true")
	}
	if got := cfg.GetMetricsAddr(); got != ":8080" {
		t.Fatalf("expected metrics-bind-address=:8080, got %q", got)
	}
	if got := cfg.GetProbeAddr(); got != ":8081" {
		t.Fatalf("expected health-probe-bind-address=:8081, got %q", got)
	}
	if got := cfg.GetMetricsServerMinSamplesRequired(); got != 15 {
		t.Fatalf("expected metrics-server-min-samples-required=15, got %d", got)
	}
	if got := cfg.GetMetricsServerSamplingInterval(); got != 30*time.Second {
		t.Fatalf("expected metrics-server-sampling-interval=30s, got %v", got)
	}
}

func TestLoadFromDirInvalidValue(t *testing.T) {
	dir := t.TempDir()
	writeConfigKey(t, dir, "dry-run", "not-a-bool")

	cfg := NewOperatorConfig()
	if err := cfg.LoadFromDir(dir); err == nil {
		t.Fatalf("expected error for invalid bool value")
	}
}

func TestLoadFromEnvParsesValues(t *testing.T) {
	cfg := NewOperatorConfig()

	t.Setenv("OPTIPOD_DRY_RUN", "true")
	t.Setenv("OPTIPOD_METRICS_PROVIDER", testProviderPrometheus)
	t.Setenv("OPTIPOD_RECONCILIATION_INTERVAL", "3m")
	t.Setenv("OPTIPOD_METRICS_SERVER_MIN_SAMPLES_REQUIRED", "7")
	t.Setenv("OPTIPOD_METRICS_BIND_ADDRESS", ":9090")

	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if !cfg.IsDryRun() {
		t.Fatalf("expected dry-run=true from env")
	}
	if got := cfg.GetMetricsProvider(); got != testProviderPrometheus {
		t.Fatalf("expected metrics provider prometheus from env, got %q", got)
	}
	if got := cfg.GetReconciliationInterval(); got != 3*time.Minute {
		t.Fatalf("expected reconciliation interval 3m from env, got %v", got)
	}
	if got := cfg.GetMetricsServerMinSamplesRequired(); got != 7 {
		t.Fatalf("expected min samples 7 from env, got %d", got)
	}
	if got := cfg.GetMetricsAddr(); got != ":9090" {
		t.Fatalf("expected metrics bind address :9090 from env, got %q", got)
	}
}

func TestLoadFromEnvInvalidValue(t *testing.T) {
	cfg := NewOperatorConfig()
	t.Setenv("OPTIPOD_DRY_RUN", "not-bool")

	if err := cfg.LoadFromEnv(); err == nil {
		t.Fatalf("expected error for invalid env bool value")
	}
}

func TestConfigPrecedenceConfigMapThenFlags(t *testing.T) {
	dir := t.TempDir()
	writeConfigKey(t, dir, "metrics-provider", testProviderPrometheus)
	writeConfigKey(t, dir, "dry-run", "true")

	cfg := NewOperatorConfig()
	if err := cfg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir returned error: %v", err)
	}

	withTestFlagSet(t, func(fs *flag.FlagSet) {
		cfg.BindFlags()
		if err := fs.Parse([]string{"--metrics-provider=metrics-server", "--dry-run=false"}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
	})

	if got := cfg.GetMetricsProvider(); got != "metrics-server" {
		t.Fatalf("expected flag value to override ConfigMap for metrics-provider, got %q", got)
	}
	if cfg.IsDryRun() {
		t.Fatalf("expected flag value to override ConfigMap for dry-run")
	}
}

func TestConfigPrecedenceConfigMapThenEnvThenFlags(t *testing.T) {
	dir := t.TempDir()
	writeConfigKey(t, dir, "metrics-provider", "metrics-server")

	cfg := NewOperatorConfig()
	if err := cfg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir returned error: %v", err)
	}

	t.Setenv("OPTIPOD_METRICS_PROVIDER", testProviderPrometheus)
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	withTestFlagSet(t, func(fs *flag.FlagSet) {
		cfg.BindFlags()
		if err := fs.Parse([]string{"--metrics-provider=custom"}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
	})

	if got := cfg.GetMetricsProvider(); got != "custom" {
		t.Fatalf("expected flags to win over env and configmap, got %q", got)
	}
}

func TestConfigPrecedenceDefaultsThenConfigMap(t *testing.T) {
	dir := t.TempDir()
	writeConfigKey(t, dir, "metrics-provider", testProviderPrometheus)

	cfg := NewOperatorConfig()
	if err := cfg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir returned error: %v", err)
	}

	withTestFlagSet(t, func(fs *flag.FlagSet) {
		cfg.BindFlags()
		if err := fs.Parse([]string{}); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
	})

	if got := cfg.GetMetricsProvider(); got != testProviderPrometheus {
		t.Fatalf("expected ConfigMap value to override defaults, got %q", got)
	}
}

func withTestFlagSet(t *testing.T, fn func(*flag.FlagSet)) {
	t.Helper()
	original := flag.CommandLine
	testSet := flag.NewFlagSet("test", flag.ContinueOnError)
	flag.CommandLine = testSet
	t.Cleanup(func() {
		flag.CommandLine = original
	})
	fn(testSet)
}

func writeConfigKey(t *testing.T, dir, key, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, key), []byte(value), 0o600); err != nil {
		t.Fatalf("failed to write key %q: %v", key, err)
	}
}
