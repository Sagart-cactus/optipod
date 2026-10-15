package framework

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

const (
	// ControllerMetricsServiceName is the shipped, name-prefixed Service
	// (config/default/metrics_service.yaml) that fronts the deployed
	// controller's metrics endpoint.
	ControllerMetricsServiceName = "optipod-controller-manager-metrics-service"
	// ControllerMetricsServicePort is the Service port that fronts the
	// controller pod's HTTPS metrics port (8443 -> targetPort 8080).
	ControllerMetricsServicePort = 8443
	// MetricsReaderClusterRoleName is the shipped, name-prefixed ClusterRole
	// (config/rbac/metrics_reader_role.yaml) granting nonResourceURL /metrics
	// get. The shipped manifests define the role and deliberately leave it
	// unbound: a cluster operator binds it to the identity their Prometheus
	// authenticates as. The harness performs exactly that documented step for
	// the metrics fixture's service account
	// (FixtureMetricsReaderBindingName), so the scrape is authorized through
	// the shipped role rather than a compensating ad hoc grant.
	MetricsReaderClusterRoleName = "optipod-metrics-reader"
)

// ControllerMetricsURL returns the in-cluster URL of the deployed controller's
// metrics endpoint — the same target the shipped ServiceMonitor
// (config/prometheus/monitor.yaml) scrapes.
func ControllerMetricsURL() string {
	return fmt.Sprintf("https://%s.%s.svc:%d/metrics",
		ControllerMetricsServiceName, SystemNamespace, ControllerMetricsServicePort)
}

// ControllerMetricsScraper reads the deployed controller's Prometheus
// exposition through the supported metrics access path, consumed exactly as
// the shipped ServiceMonitor consumes it: an in-cluster HTTPS scrape of the
// shipped controller-manager-metrics-service, authenticated with a service
// account token bound to the shipped metrics-reader ClusterRole, against the
// metrics endpoint's own authentication/authorization filter. The scrape runs
// from the in-cluster metrics fixture because the API server strips caller
// credentials from service-proxy requests, so no out-of-cluster client can
// present a bearer token to the filtered endpoint; an in-cluster scrape is
// where a real Prometheus agent runs. The fixture never logs or persists the
// token, and the exposition it relays contains no credentials.
type ControllerMetricsScraper struct {
	fixture *MetricsFixtureClient
}

// NewControllerMetricsScraper builds the scraper over the harness-installed
// metrics fixture. It creates no per-spec identity: the fixture's service
// account and its binding to the shipped metrics-reader ClusterRole are part
// of the owned cluster's install, so ScrapingIdentity is API-observable for
// the suite's lifetime.
func NewControllerMetricsScraper(clients *Clients) (*ControllerMetricsScraper, error) {
	if clients == nil || clients.Kube == nil {
		return nil, fmt.Errorf("controller metrics scraper: clients are required")
	}
	fixture, err := NewMetricsFixtureClient(clients)
	if err != nil {
		return nil, fmt.Errorf("controller metrics scraper: %w", err)
	}
	return &ControllerMetricsScraper{fixture: fixture}, nil
}

// Scrape returns the controller's full Prometheus exposition verbatim, scraped
// in-cluster through the shipped metrics access path.
func (s *ControllerMetricsScraper) Scrape(ctx context.Context) (string, error) {
	if s == nil || s.fixture == nil {
		return "", fmt.Errorf("controller metrics scraper: not initialized")
	}
	return s.fixture.ScrapeControllerMetrics(ctx)
}

// ScrapingIdentity returns the API-observable identity the in-cluster scrape
// authenticates as — the fixture's service account — plus the name of the
// harness ClusterRoleBinding that grants it the shipped metrics-reader role.
// It exposes names only; the bearer token has no accessor, so no failure
// message can ever dump it into JUnit or console output.
func (s *ControllerMetricsScraper) ScrapingIdentity() (types.NamespacedName, string) {
	return types.NamespacedName{Namespace: FixtureNamespace, Name: FixtureServiceAccountName},
		FixtureMetricsReaderBindingName
}

// metricLineRe matches one Prometheus text-format sample line:
// name{label="value",...} value [timestamp].
var metricLineRe = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{(.*)\})?\s+([-+0-9.eE]+|NaN)`)
var labelPairRe = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="([^"]*)"`)

// MetricsSampleValue finds the sample of the named metric whose labels contain
// every wanted label pair and returns its value. The boolean reports whether a
// matching sample exists. Label matching is exact; only lines carrying ALL the
// wanted pairs count (a metric like a histogram emits many series per name).
func MetricsSampleValue(exposition, metricName string, wantLabels map[string]string) (float64, bool, error) {
	for _, line := range strings.Split(exposition, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := metricLineRe.FindStringSubmatch(line)
		if m == nil || m[1] != metricName {
			continue
		}
		if len(wantLabels) > 0 && m[3] == "" {
			continue
		}
		if !labelsMatch(m[3], wantLabels) {
			continue
		}
		value, err := strconv.ParseFloat(m[4], 64)
		if err != nil {
			return 0, false, fmt.Errorf("parse metrics sample %q from line %q: %w", m[4], line, err)
		}
		return value, true, nil
	}
	return 0, false, nil
}

// MetricsFamiliesPresent reports whether the exposition declares every named
// metric family (a HELP or TYPE comment line). A family is registered even
// before its first labeled sample exists, so family presence is the
// pre-reconcile observable; labeled samples are the post-reconcile one.
func MetricsFamiliesPresent(exposition string, families ...string) bool {
	for _, family := range families {
		declared := false
		for _, line := range strings.Split(exposition, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") &&
				(strings.Contains(trimmed, "# HELP "+family+" ") ||
					strings.Contains(trimmed, "# TYPE "+family+" ")) {
				declared = true
				break
			}
		}
		if !declared {
			return false
		}
	}
	return true
}

func labelsMatch(labelSection string, want map[string]string) bool {
	for key, value := range want {
		found := false
		for _, pair := range labelPairRe.FindAllStringSubmatch(labelSection, -1) {
			if pair[1] == key && pair[2] == value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
