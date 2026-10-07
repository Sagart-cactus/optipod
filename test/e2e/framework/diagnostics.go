package framework

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	admissionregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

const defaultDiagnosticsTimeout = 2 * time.Minute

// harnessNamespaceSelector matches namespaces created by the Factory; the
// failure diagnostics sweep covers exactly these plus the system namespace.
const harnessNamespaceSelector = "app.kubernetes.io/managed-by=optipod-e2e-harness"

// DiagnosticsContext returns a bounded context for diagnostic collection,
// derived from context.Background so collection still proceeds after a
// cancelled or timed-out run context, with its own independent deadline.
func DiagnosticsContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultDiagnosticsTimeout)
}

// ListHarnessNamespaces returns the namespaces the harness created this run
// (every spec namespace comes from Factory.CreateNamespace). The failure
// diagnostics sweep uses this list so a failed spec's own resources are
// captured before teardown removes the cluster.
func ListHarnessNamespaces(ctx context.Context, clients *Clients) ([]string, error) {
	names, err := clients.Kube.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: harnessNamespaceSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list harness namespaces: %w", err)
	}
	out := make([]string, 0, len(names.Items))
	for _, ns := range names.Items {
		out = append(out, ns.Name)
	}
	sort.Strings(out)
	return out, nil
}

// DiagnosticsScope names what a diagnostic bundle must cover. Scope comes from
// the failing run itself: the harness-created namespaces, the failure summary,
// and the pinned inputs for provenance.
type DiagnosticsScope struct {
	// Namespaces are the harness-created namespaces to sweep (typically
	// ListHarnessNamespaces plus SystemNamespace and FixtureNamespace).
	Namespaces []string
	// FailureSummary is the original failure text; it stays identifiable in
	// the bundle so the diagnostic evidence cannot be mistaken for the cause.
	FailureSummary string
	// ClusterName is the owned cluster the bundle came from.
	ClusterName string
	// Pins records the pinned tool/image inputs for provenance.
	Pins []Pin
}

// DiagnosticBundle reports what was collected. Collection errors are recorded
// rather than aborting the bundle: a partial bundle with named gaps is more
// useful than none, and the original failure remains the primary error.
type DiagnosticBundle struct {
	Dir              string   `json:"dir"`
	Files            []string `json:"files"`
	Redactions       int      `json:"redactions"`
	CollectionErrors []string `json:"collectionErrors,omitempty"`
}

// CollectDiagnostics gathers a redacted, actionable diagnostic bundle from the
// owned cluster: the original failure text, controller logs, scoped events,
// relevant object YAML/status (policies, workloads, pods), pod/deployment
// state, webhook configuration and CA metadata (never key material), metrics
// fixture trace, and Secret/ConfigMap metadata. It must be called BEFORE the
// owned cluster is torn down. Kubeconfig contents, Secret values, private keys,
// bearer tokens and credentials are structurally excluded and additionally
// scrubbed by Redact.
func CollectDiagnostics(ctx context.Context, clients *Clients, scope DiagnosticsScope, outDir string) (*DiagnosticBundle, error) {
	if clients == nil {
		return nil, fmt.Errorf("diagnostics need typed clients from the private kubeconfig")
	}
	if outDir == "" {
		return nil, fmt.Errorf("diagnostics need an output directory")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create diagnostic bundle dir %s: %w", outDir, err)
	}
	c := &collector{
		clients: clients,
		scope:   scope,
		bundle:  &DiagnosticBundle{Dir: outDir},
	}

	// The original failure stays identifiable at the top of the bundle.
	if strings.TrimSpace(scope.FailureSummary) != "" {
		c.writeText("failures.txt", scope.FailureSummary+"\n")
	}

	c.collectSystemState(ctx)
	for _, ns := range scope.Namespaces {
		c.collectNamespace(ctx, ns)
	}

	c.writeSummary()
	if len(c.bundle.CollectionErrors) > 0 {
		c.writeText("collection-errors.txt",
			"Errors encountered while collecting this bundle (the original failure above remains the primary error):\n"+
				strings.Join(c.bundle.CollectionErrors, "\n")+"\n")
	}
	return c.bundle, nil
}

// collector accumulates one diagnostic bundle, isolating per-step failures.
type collector struct {
	clients *Clients
	scope   DiagnosticsScope
	bundle  *DiagnosticBundle
}

func (c *collector) recordError(format string, args ...any) {
	c.bundle.CollectionErrors = append(c.bundle.CollectionErrors,
		redactError(fmt.Errorf(format, args...)))
}

// writeText writes redacted text content into the bundle.
func (c *collector) writeText(rel, content string) {
	path := filepath.Join(c.bundle.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		c.recordError("create %s: %v", rel, err)
		return
	}
	scrubbed, n := RedactCount(content)
	c.bundle.Redactions += n
	if err := os.WriteFile(path, []byte(scrubbed), 0o644); err != nil {
		c.recordError("write %s: %v", rel, err)
		return
	}
	c.bundle.Files = append(c.bundle.Files, rel)
}

// writeYAML marshals obj through YAML and writes the redacted result.
func (c *collector) writeYAML(rel string, obj interface{}) {
	raw, err := yaml.Marshal(obj)
	if err != nil {
		c.recordError("marshal %s: %v", rel, err)
		return
	}
	c.writeText(rel, string(raw))
}

// writeJSON marshals obj through JSON and writes the redacted result.
func (c *collector) writeJSON(rel string, obj interface{}) {
	raw, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		c.recordError("marshal %s: %v", rel, err)
		return
	}
	c.writeText(rel, string(raw)+"\n")
}

// collectSystemState captures the installed control plane: controller logs,
// deployment/pod state, webhook configuration with CA metadata only, webhook
// service/endpoints and the metrics fixture trace.
func (c *collector) collectSystemState(ctx context.Context) {
	// Controller logs first: they are the most perishable evidence.
	pods, err := c.clients.Kube.CoreV1().Pods(SystemNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: ControllerPodSelector,
	})
	if err != nil {
		c.recordError("list controller pods: %v", err)
	} else {
		for i := range pods.Items {
			pod := pods.Items[i]
			logs, err := c.clients.Kube.CoreV1().Pods(SystemNamespace).
				GetLogs(pod.Name, &corev1.PodLogOptions{Container: ControllerContainerName, Timestamps: true}).DoRaw(ctx)
			if err != nil {
				c.recordError("read controller logs for %s: %v", pod.Name, err)
			} else {
				c.writeText(filepath.Join("system", "controller-logs", pod.Name+".log"), string(logs))
			}
			prev, err := c.clients.Kube.CoreV1().Pods(SystemNamespace).
				GetLogs(pod.Name, &corev1.PodLogOptions{Container: ControllerContainerName, Previous: true, Timestamps: true}).DoRaw(ctx)
			if err == nil {
				c.writeText(filepath.Join("system", "controller-logs", pod.Name+".previous.log"), string(prev))
			} else if !apierrors.IsBadRequest(err) && !apierrors.IsNotFound(err) {
				// No previous container is normal for a running pod.
				c.recordError("read previous controller logs for %s: %v", pod.Name, err)
			}
		}
	}

	if dep, err := c.clients.Kube.AppsV1().Deployments(SystemNamespace).Get(ctx, ControllerDeploymentName, metav1.GetOptions{}); err != nil {
		c.recordError("get controller deployment: %v", err)
	} else {
		c.writeYAML(filepath.Join("system", "controller-deployment.yaml"), dep)
	}
	c.listToYAML("system", "controller-pods", func() (interface{}, error) {
		return c.clients.Kube.CoreV1().Pods(SystemNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: ControllerPodSelector,
		})
	})

	// Webhook configuration: rules, selectors, failure policy and CA metadata.
	// The CA bundle itself is a public certificate, but only its metadata is
	// recorded; key material never exists in this object.
	if mwc, err := c.clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, WebhookConfigurationName, metav1.GetOptions{}); err != nil {
		c.recordError("get mutating webhook configuration: %v", err)
	} else {
		c.writeYAML(filepath.Join("system", "webhook-configuration.yaml"), webhookConfigurationDiagnosticsFrom(mwc))
	}
	c.getToYAML("webhook-service", func() (interface{}, error) {
		return c.clients.Kube.CoreV1().Services(SystemNamespace).Get(ctx, WebhookServiceName, metav1.GetOptions{})
	})
	c.getToYAML("webhook-endpoints", func() (interface{}, error) {
		return c.clients.Kube.CoreV1().Endpoints(SystemNamespace).Get(ctx, WebhookServiceName, metav1.GetOptions{})
	})
	c.getToYAML("fixture-deployment", func() (interface{}, error) {
		return c.clients.Kube.AppsV1().Deployments(FixtureNamespace).Get(ctx, FixtureServiceName, metav1.GetOptions{})
	})
	c.getToYAML("fixture-endpoints", func() (interface{}, error) {
		return c.clients.Kube.CoreV1().Endpoints(FixtureNamespace).Get(ctx, FixtureServiceName, metav1.GetOptions{})
	})

	// What the deployed controller actually asked the fixture for.
	trace, err := NewMetricsFixtureClient(c.clients).Trace(ctx)
	if err != nil {
		c.recordError("read metrics fixture trace: %v", err)
	} else {
		c.writeJSON(filepath.Join("system", "metrics-fixture-trace.json"), trace)
	}
}

// collectNamespace sweeps one namespace: policies, workloads, pods, services,
// events and Secret/ConfigMap metadata (values are never written).
func (c *collector) collectNamespace(ctx context.Context, ns string) {
	policies := &optipodv1alpha1.OptimizationPolicyList{}
	if err := c.clients.Ctrl.List(ctx, policies, client.InNamespace(ns)); err != nil {
		c.recordError("list optimizationpolicies in %s: %v", ns, err)
	} else {
		c.writeYAML(filepath.Join("namespaces", ns, "optimizationpolicies.yaml"), policies)
	}
	c.listToYAML(filepath.Join("namespaces", ns), "deployments", func() (interface{}, error) {
		return c.clients.Kube.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	})
	c.listToYAML(filepath.Join("namespaces", ns), "statefulsets", func() (interface{}, error) {
		return c.clients.Kube.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	})
	c.listToYAML(filepath.Join("namespaces", ns), "daemonsets", func() (interface{}, error) {
		return c.clients.Kube.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
	})
	c.listToYAML(filepath.Join("namespaces", ns), "replicasets", func() (interface{}, error) {
		return c.clients.Kube.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	})
	c.listToYAML(filepath.Join("namespaces", ns), "pods", func() (interface{}, error) {
		return c.clients.Kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	})
	c.listToYAML(filepath.Join("namespaces", ns), "services", func() (interface{}, error) {
		return c.clients.Kube.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	})
	// ConfigMaps go through the redaction scrubber: controller configuration
	// supports credential-shaped keys (prometheus-password, prometheus-bearer-token).
	c.listToYAML(filepath.Join("namespaces", ns), "configmaps", func() (interface{}, error) {
		return c.clients.Kube.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	})

	if events, err := c.clients.Kube.CoreV1().Events(ns).List(ctx, metav1.ListOptions{}); err != nil {
		c.recordError("list events in %s: %v", ns, err)
	} else {
		sort.SliceStable(events.Items, func(i, j int) bool {
			return events.Items[i].LastTimestamp.Time.Before(events.Items[j].LastTimestamp.Time)
		})
		c.writeJSON(filepath.Join("namespaces", ns, "events.json"), events)
	}

	if secrets, err := c.clients.Kube.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{}); err != nil {
		c.recordError("list secrets metadata in %s: %v", ns, err)
	} else {
		// Secret NAMES and key names prove the objects exist and were
		// deliberately excluded; values are never read into the bundle.
		meta := make([]map[string]any, 0, len(secrets.Items))
		for _, s := range secrets.Items {
			keys := make([]string, 0, len(s.Data)+len(s.StringData))
			for k := range s.Data {
				keys = append(keys, k)
			}
			for k := range s.StringData {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			meta = append(meta, map[string]any{
				"name":              s.Name,
				"namespace":         s.Namespace,
				"type":              string(s.Type),
				"dataKeys":          keys,
				"creationTimestamp": s.CreationTimestamp.UTC().Format(time.RFC3339),
			})
		}
		c.writeJSON(filepath.Join("namespaces", ns, "secrets-metadata.json"), map[string]any{
			"note":    "Secret values are structurally excluded from diagnostics; only names, types and key names are recorded.",
			"secrets": meta,
		})
	}
}

func (c *collector) listToYAML(dir, name string, list func() (interface{}, error)) {
	obj, err := list()
	if err != nil {
		c.recordError("list %s: %v", name, err)
		return
	}
	c.writeYAML(filepath.Join(dir, name+".yaml"), obj)
}

func (c *collector) getToYAML(name string, get func() (interface{}, error)) {
	obj, err := get()
	if err != nil {
		c.recordError("get %s: %v", name, err)
		return
	}
	c.writeYAML(filepath.Join("system", name+".yaml"), obj)
}

func (c *collector) writeSummary() {
	c.writeJSON("summary.json", map[string]any{
		"cluster":     c.scope.ClusterName,
		"collectedAt": time.Now().UTC().Format(time.RFC3339),
		"namespaces":  c.scope.Namespaces,
		"pins":        c.scope.Pins,
		"fileCount":   len(c.bundle.Files),
		"redactions":  c.bundle.Redactions,
		"exclusions":  "kubeconfig contents, Secret values, private keys, bearer tokens and credentials are never collected; all text is additionally scrubbed",
		"files":       c.bundle.Files,
	})
}

// webhookConfigurationDiagnostics is the redacted-by-construction form of the
// installed MutatingWebhookConfiguration: admission rules, selectors, failure
// policy, service wiring and CA metadata. The raw caBundle bytes are dropped
// in favour of certificate metadata; key material never exists here.
type webhookConfigurationDiagnostics struct {
	Name     string               `json:"name"`
	Labels   map[string]string    `json:"labels,omitempty"`
	Webhooks []webhookDiagnostics `json:"webhooks"`
}

type webhookDiagnostics struct {
	Name              string                              `json:"name"`
	FailurePolicy     string                              `json:"failurePolicy"`
	MatchPolicy       string                              `json:"matchPolicy,omitempty"`
	NamespaceSelector *metav1.LabelSelector               `json:"namespaceSelector,omitempty"`
	ObjectSelector    *metav1.LabelSelector               `json:"objectSelector,omitempty"`
	Rules             []admissionregv1.RuleWithOperations `json:"rules"`
	ServiceNamespace  string                              `json:"serviceNamespace"`
	ServiceName       string                              `json:"serviceName"`
	ServicePort       int32                               `json:"servicePort"`
	Path              *string                             `json:"path,omitempty"`
	TimeoutSeconds    int32                               `json:"timeoutSeconds"`
	CABundleMetadata  map[string]any                      `json:"caBundleMetadata"`
}

func webhookConfigurationDiagnosticsFrom(mwc *admissionregv1.MutatingWebhookConfiguration) webhookConfigurationDiagnostics {
	out := webhookConfigurationDiagnostics{
		Name:     mwc.Name,
		Labels:   mwc.Labels,
		Webhooks: make([]webhookDiagnostics, 0, len(mwc.Webhooks)),
	}
	for _, wh := range mwc.Webhooks {
		wd := webhookDiagnostics{
			Name:              wh.Name,
			FailurePolicy:     string(*wh.FailurePolicy),
			NamespaceSelector: wh.NamespaceSelector,
			ObjectSelector:    wh.ObjectSelector,
			Rules:             wh.Rules,
			TimeoutSeconds:    *wh.TimeoutSeconds,
			CABundleMetadata:  caBundleMetadata(wh.ClientConfig.CABundle),
		}
		if wh.MatchPolicy != nil {
			wd.MatchPolicy = string(*wh.MatchPolicy)
		}
		if wh.ClientConfig.Service != nil {
			wd.ServiceNamespace = wh.ClientConfig.Service.Namespace
			wd.ServiceName = wh.ClientConfig.Service.Name
			if wh.ClientConfig.Service.Port != nil {
				wd.ServicePort = *wh.ClientConfig.Service.Port
			}
			wd.Path = wh.ClientConfig.Service.Path
		}
		out.Webhooks = append(out.Webhooks, wd)
	}
	return out
}

// caBundleMetadata describes the webhook CA bundle as public certificate
// metadata: subject, issuer, SANs, validity and fingerprint — never key
// material, and not the raw bundle bytes. The []byte caBundle read through
// the API already carries the decoded PEM (base64 is only the JSON wire
// form of the field).
func caBundleMetadata(bundle []byte) map[string]any {
	out := map[string]any{
		"bundleBytes": len(bundle),
	}
	if len(bundle) == 0 {
		out["present"] = false
		return out
	}
	out["present"] = true
	block, _ := pem.Decode(bundle)
	if block == nil {
		out["pemError"] = "caBundle is not PEM-encoded"
		return out
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		out["parseError"] = redactError(err)
		return out
	}
	fingerprint := sha256.Sum256(cert.Raw)
	out["subject"] = cert.Subject.String()
	out["issuer"] = cert.Issuer.String()
	out["dnsNames"] = cert.DNSNames
	out["notBefore"] = cert.NotBefore.UTC().Format(time.RFC3339)
	out["notAfter"] = cert.NotAfter.UTC().Format(time.RFC3339)
	out["sha256Fingerprint"] = fmt.Sprintf("%x", fingerprint)
	return out
}
