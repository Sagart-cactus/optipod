package consoleinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/optipod/optipod/internal/consoletls"
	"github.com/optipod/optipod/internal/consoleversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
type Report struct {
	Checks []Check `json:"checks"`
}

func (r *Report) Add(name, status, message string) {
	r.Checks = append(r.Checks, Check{name, status, message})
}
func (r Report) Ready() bool {
	for _, c := range r.Checks {
		if c.Status == "fail" {
			return false
		}
	}
	return true
}

type ProbeOptions struct {
	ServerURL, CAPEM, MetricsSource, PrometheusURL, PrometheusAuth, PrometheusAuthDir, PrometheusCAFile, VolumePath string
	Namespaces                                                                                                      []string
	AllowHTTP                                                                                                       bool
}

// Probe runs inside the target cluster, exercising its DNS, TLS, egress,
// selected metrics API and actual dynamically provisioned queue volume.
func Probe(ctx context.Context, o ProbeOptions) Report {
	r := Report{Checks: []Check{}}
	client, err := consoletls.Client([]byte(o.CAPEM), nil, nil)
	if err != nil {
		r.Add("console-tls", "fail", "Invalid console CA bundle")
		return r
	}
	defer client.CloseIdleConnections()
	u, err := url.Parse(o.ServerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != httpsScheme && (!o.AllowHTTP || u.Scheme != httpScheme)) {
		r.Add("console-tls", "fail", "An HTTPS console origin is required")
		return r
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(o.ServerURL, "/")+"/api/version", nil)
	if err != nil {
		r.Add("console-tls", "fail", "Invalid console endpoint")
		return r
	}
	response, err := client.Do(request)
	if err != nil {
		r.Add("console-tls", "fail", "Console DNS, routing or TLS verification failed from the cluster; verify CA, hostname and synchronized node clocks")
	} else {
		var version struct {
			Version  string `json:"version"`
			Protocol int    `json:"protocol"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&version)
		_ = response.Body.Close()
		if response.StatusCode != 200 || err != nil || version.Protocol != 1 || !consoleversion.Compatible(version.Version) {
			r.Add("console-version", "fail", "Console and installer versions are incompatible")
		} else {
			r.Add("console-tls", "pass", "Console endpoint and compatibility checks passed")
		}
	}
	if o.MetricsSource == "prometheus" {
		probePrometheus(ctx, &r, o)
	} else {
		probeMetricsServer(ctx, &r, o.Namespaces)
	}
	if o.VolumePath != "" {
		file, err := os.CreateTemp(o.VolumePath, ".probe-")
		if err == nil {
			_, err = file.WriteString("optipod-volume-probe")
			if err == nil {
				err = file.Sync()
			}
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
		if err != nil {
			r.Add("storage", "fail", "Queue PVC is not writable by the agent user")
		} else {
			r.Add("storage", "pass", "Queue PVC provisioned, mounted and fsynced")
		}
	}
	return r
}

func probeMetricsServer(ctx context.Context, r *Report, namespaces []string) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		r.Add("metrics-server", "fail", "In-cluster metrics credentials unavailable")
		return
	}
	c, err := metricsclient.NewForConfig(cfg)
	if err != nil {
		r.Add("metrics-server", "fail", "Cannot initialize metrics API client")
		return
	}
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	count := 0
	for _, ns := range namespaces {
		metrics, err := c.MetricsV1beta1().PodMetricses(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			r.Add("metrics-server", "fail", "Pod metrics API denied or unavailable; check metrics-server/APIService and RBAC")
			return
		}
		if len(metrics.Items) == 0 {
			r.Add("metrics-server", "fail", "No metrics available in namespace "+ns)
			return
		}
		for _, pod := range metrics.Items {
			for _, container := range pod.Containers {
				if _, cpu := container.Usage["cpu"]; cpu {
					if _, memory := container.Usage["memory"]; memory {
						count++
					}
				}
			}
		}
	}
	if count == 0 {
		r.Add("metrics-server", "fail", "No CPU/memory samples in the selected namespaces; start a workload and wait for metrics collection")
		return
	}
	r.Add("metrics-server", "pass", fmt.Sprintf("CPU and memory samples available for %d containers", count))
	r.Add("history", "warn", "Metrics-server supplies current samples; Recommend mode must collect the configured minimum history before recommendations become eligible")
}

func probePrometheus(ctx context.Context, r *Report, o ProbeOptions) {
	u, err := url.Parse(o.PrometheusURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != httpScheme && u.Scheme != httpsScheme) {
		r.Add("prometheus", "fail", "Configure a valid Prometheus URL without embedded credentials")
		return
	}
	var ca []byte
	if o.PrometheusCAFile != "" {
		ca, err = os.ReadFile(o.PrometheusCAFile)
		if err != nil {
			r.Add("prometheus", "fail", "Cannot read configured Prometheus CA")
			return
		}
	}
	c, err := consoletls.Client(ca, nil, nil)
	if err != nil {
		r.Add("prometheus", "fail", "Invalid Prometheus CA")
		return
	}
	defer c.CloseIdleConnections()
	names := o.Namespaces
	if len(names) == 0 {
		names = []string{""}
	}
	for _, ns := range names {
		selector := "container!=\"\""
		if ns != "" {
			selector += ",namespace=" + strconv.Quote(ns)
		}
		queries := []struct {
			name, query string
			minimum     float64
		}{{"prometheus-cpu", "count(rate(container_cpu_usage_seconds_total{" + selector + "}[2m]))", 1}, {"prometheus-memory", "count(container_memory_working_set_bytes{" + selector + "})", 1}, {"history", "min(count_over_time(container_cpu_usage_seconds_total{" + selector + "}[15m]))", 10}}
		for _, query := range queries {
			value, err := prometheusQuery(ctx, c, o, query.query)
			name := query.name
			if ns != "" {
				name += "/" + ns
			}
			if err != nil {
				r.Add(name, "fail", "Prometheus query failed; check authentication, TLS and container metric scraping")
				continue
			}
			if value < query.minimum {
				status := "fail"
				if query.name == "history" {
					status = "warn"
				}
				r.Add(name, status, "Missing samples or insufficient history; keep Recommend mode while metrics accumulate")
			} else {
				r.Add(name, "pass", "Required container metrics and sample coverage available")
			}
		}
	}
}

func prometheusQuery(ctx context.Context, c *http.Client, o ProbeOptions, query string) (float64, error) {
	endpoint := strings.TrimSuffix(o.PrometheusURL, "/") + "/api/v1/query?" + url.Values{"query": []string{query}}.Encode()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	switch o.PrometheusAuth {
	case bearerAuth:
		token, err := os.ReadFile(filepath.Join(o.PrometheusAuthDir, "token"))
		if err != nil {
			return 0, err
		}
		r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	case basicAuth:
		user, err := os.ReadFile(filepath.Join(o.PrometheusAuthDir, "username"))
		if err != nil {
			return 0, err
		}
		password, err := os.ReadFile(filepath.Join(o.PrometheusAuthDir, "password"))
		if err != nil {
			return 0, err
		}
		r.SetBasicAuth(strings.TrimSpace(string(user)), strings.TrimSpace(string(password)))
	}
	response, err := c.Do(r)
	if err != nil {
		return 0, fmt.Errorf("prometheus unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	var result struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result) != nil || result.Status != "success" {
		return 0, fmt.Errorf("prometheus query rejected")
	}
	if len(result.Data.Result) == 0 {
		return 0, nil
	}
	if len(result.Data.Result[0].Value) != 2 {
		return 0, fmt.Errorf("invalid sample")
	}
	var raw string
	if json.Unmarshal(result.Data.Result[0].Value[1], &raw) != nil {
		return 0, fmt.Errorf("invalid sample")
	}
	value, err := strconv.ParseFloat(raw, 64)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("invalid sample")
	}
	return value, err
}
