package framework

import (
	"context"
	"testing"
)

const sampleExposition = `# HELP optipod_workloads_monitored Number of workloads currently monitored by OptiPod
# TYPE optipod_workloads_monitored gauge
# HELP optipod_reconciliation_duration_seconds Duration of reconciliation cycles in seconds
# TYPE optipod_reconciliation_duration_seconds histogram
optipod_reconciliation_duration_seconds_bucket{policy="p-old",le="0.005"} 1
optipod_reconciliation_duration_seconds_count{policy="p-old"} 1
optipod_reconciliation_duration_seconds_sum{policy="p-old"} 0.0025
optipod_reconciliation_duration_seconds_bucket{policy="p-new",le="0.005"} 3
optipod_reconciliation_duration_seconds_count{policy="p-new"} 3
optipod_reconciliation_duration_seconds_sum{policy="p-new"} 0.0075
# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 42
`

func TestMetricsSampleValueFindsLabeledSample(t *testing.T) {
	value, ok, err := MetricsSampleValue(sampleExposition, "optipod_reconciliation_duration_seconds_count",
		map[string]string{"policy": "p-new"})
	if err != nil {
		t.Fatalf("MetricsSampleValue returned error: %v", err)
	}
	if !ok {
		t.Fatal("MetricsSampleValue did not find the labeled sample")
	}
	if value != 3 {
		t.Fatalf("MetricsSampleValue = %v, want 3", value)
	}
}

func TestMetricsSampleValueDistinguishesLabelValues(t *testing.T) {
	value, ok, err := MetricsSampleValue(sampleExposition, "optipod_reconciliation_duration_seconds_count",
		map[string]string{"policy": "p-old"})
	if err != nil {
		t.Fatalf("MetricsSampleValue returned error: %v", err)
	}
	if !ok || value != 1 {
		t.Fatalf("MetricsSampleValue = (%v, %v), want (1, true)", value, ok)
	}
}

func TestMetricsSampleValueReportsMissingSample(t *testing.T) {
	_, ok, err := MetricsSampleValue(sampleExposition, "optipod_workloads_monitored",
		map[string]string{"namespace": "ns", "policy": "absent"})
	if err != nil {
		t.Fatalf("MetricsSampleValue returned error: %v", err)
	}
	if ok {
		t.Fatal("MetricsSampleValue reported a sample that the exposition does not contain")
	}
}

func TestMetricsSampleValueUnlabeledSample(t *testing.T) {
	value, ok, err := MetricsSampleValue(sampleExposition, "go_goroutines", nil)
	if err != nil {
		t.Fatalf("MetricsSampleValue returned error: %v", err)
	}
	if !ok || value != 42 {
		t.Fatalf("MetricsSampleValue = (%v, %v), want (42, true)", value, ok)
	}
}

func TestMetricsFamiliesPresent(t *testing.T) {
	if !MetricsFamiliesPresent(sampleExposition,
		"optipod_workloads_monitored", "optipod_reconciliation_duration_seconds") {
		t.Fatal("MetricsFamiliesPresent denied a declared family")
	}
	if MetricsFamiliesPresent(sampleExposition, "optipod_applications_total") {
		t.Fatal("MetricsFamiliesPresent accepted a family the exposition does not declare")
	}
	// A family prefix must not satisfy a longer family name.
	if MetricsFamiliesPresent(sampleExposition, "optipod_reconciliation_duration_seconds_bogus") {
		t.Fatal("MetricsFamiliesPresent accepted a family via a name-prefix match")
	}
}

// TestScraperRequiresClients pins the constructor's fail-fast contract: no
// clients, no scraper, no API interaction.
func TestScraperRequiresClients(t *testing.T) {
	if _, err := NewControllerMetricsScraper(nil); err == nil {
		t.Fatal("NewControllerMetricsScraper accepted nil clients")
	}
	if _, err := NewControllerMetricsScraper(&Clients{}); err == nil {
		t.Fatal("NewControllerMetricsScraper accepted clients without a Kubernetes clientset")
	}
}

// TestScraperRejectsUninitializedScrape pins that a scraper without a fixture
// client (never initialized) cannot issue a scrape.
func TestScraperRejectsUninitializedScrape(t *testing.T) {
	s := &ControllerMetricsScraper{}
	if _, err := s.Scrape(context.Background()); err == nil {
		t.Fatal("Scrape succeeded on an uninitialized scraper")
	}
}

// TestScrapingIdentityExposesNamesOnly pins that the only exported accessor
// over the scraping identity returns the API-observable names: the fixture's
// service account key and the harness binding to the shipped metrics-reader
// ClusterRole. The bearer token never leaves the fixture pod, so no failure
// message can ever dump it into JUnit or console output.
func TestScrapingIdentityExposesNamesOnly(t *testing.T) {
	s := &ControllerMetricsScraper{}
	key, binding := s.ScrapingIdentity()
	if key.Namespace != FixtureNamespace || key.Name != FixtureServiceAccountName {
		t.Fatalf("ScrapingIdentity key = %v, want %s/%s", key, FixtureNamespace, FixtureServiceAccountName)
	}
	if binding != FixtureMetricsReaderBindingName {
		t.Fatalf("ScrapingIdentity binding = %q, want %q", binding, FixtureMetricsReaderBindingName)
	}
}

// TestControllerMetricsURLTargetsShippedService pins the in-cluster scrape
// target to the shipped, name-prefixed metrics Service and port.
func TestControllerMetricsURLTargetsShippedService(t *testing.T) {
	want := "https://optipod-controller-manager-metrics-service.optipod-system.svc:8443/metrics"
	if got := ControllerMetricsURL(); got != want {
		t.Fatalf("ControllerMetricsURL = %q, want %q", got, want)
	}
}
