package suite

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves the deployed controller's supported observability
// surfaces reflect NAMED processing rather than a generic HTTP 200 or startup
// string (VAL-CONF-013):
//
//   - Metrics are scraped through the installed controller's supported metrics
//     access path, consumed exactly as the shipped ServiceMonitor
//     (config/prometheus/monitor.yaml) consumes it: an in-cluster HTTPS scrape
//     of the shipped controller-manager-metrics-service from the metrics
//     fixture, whose service account is bound to the shipped metrics-reader
//     ClusterRole and whose projected bearer token the metrics endpoint's own
//     authentication/authorization filter validates. (The API server strips
//     caller credentials from service-proxy requests, so this scrape must run
//     in-cluster, where a real Prometheus agent runs.) Before the named policy
//     exists, the exposition is a genuine registry scrape (standard Go/process
//     families) carrying no series for that policy — the OptiPod families are
//     labeled vectors, exposed only once a labeled series exists; after a
//     completed reconciliation, the policy-labeled series exist with real
//     values, and a further completed reconcile strictly increases the named
//     reconciliation counter.
//   - The controller's console-structured log records are parsed, not
//     substring-matched: a record emitted at or after the policy's creation
//     ties the policy AND workload to a processing outcome through parseable
//     JSON fields ({"policy": ..., "workload": ..., "weight": ...} and
//     {"policy": ..., "discovered": ..., "processed": ...}).
//   - The scrape content and the matched log records contain no sensitive
//     values: the harness redaction pass over them records zero replacements.
//
// No spec state depends on other specs: the policy name is unique per run, so
// before/after series assertions are attributable to this spec's named
// reconcile alone.
var _ = Describe("Supported metrics and structured logs reflect named processing", Label("conformance", "observability"), func() {
	It("exposes named OptiPod metric series and parseable structured log records for a policy reconciliation", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-observability")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the observability spec namespace")
		})
		samples := smokeSamples()

		By("creating a two-container workload whose pod becomes ready")
		workload := createSmokeWorkload(ctx, ns.Name, "conf-obs", samples)
		podSelector := "app=" + workload.Name
		beforeUIDs := podUIDs(ctx, ns.Name, podSelector)

		By("registering deterministic metric samples for the workload's pod")
		registerContainerSeries(ctx, ns.Name, workload, samples)

		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique observability policy name")

		By("verifying the in-cluster scraping identity is bound to the shipped metrics-reader role")
		// The scrape runs in-cluster from the metrics fixture (the shipped
		// ServiceMonitor's shape: projected service-account token, HTTPS, the
		// endpoint's own authentication/authorization filter) because the API
		// server strips caller credentials from service-proxy requests. The
		// shipped metrics-reader ClusterRole is deliberately unbound in the
		// shipped manifests; the harness binding to the fixture identity is the
		// documented operator step, not a compensating ad hoc grant.
		scraper, err := framework.NewControllerMetricsScraper(env.Clients)
		Expect(err).NotTo(HaveOccurred(), "create the controller metrics scraper")
		scraperIdentity, bindingName := scraper.ScrapingIdentity()
		observedSA := &corev1.ServiceAccount{}
		Expect(env.Clients.Ctrl.Get(ctx, scraperIdentity, observedSA)).To(Succeed(),
			"the in-cluster scraping identity must be API-observable before scraping")
		observedBinding := &rbacv1.ClusterRoleBinding{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Name: bindingName}, observedBinding)).To(Succeed(),
			"the harness binding for the scraping identity must be API-observable")
		Expect(observedBinding.RoleRef.Name).To(Equal(framework.MetricsReaderClusterRoleName),
			"the scrape must be authorized through the shipped metrics-reader ClusterRole")

		By("scraping the metrics endpoint before the named policy exists")
		before := scrapeUntilServing(ctx, scraper)
		// The exposition must be a genuine scrape of the controller's
		// Prometheus registry (the standard process/Go families are always
		// exported), not a bare HTTP 200. The OptiPod families are all labeled
		// vectors, which Prometheus only exposes once a labeled series exists,
		// so their appearance after the named reconcile below is itself part
		// of the named-processing proof.
		Expect(framework.MetricsFamiliesPresent(before, "go_goroutines")).To(BeTrue(),
			"the scrape did not return a genuine Prometheus exposition from the controller's registry; a bare HTTP 200 is not observability proof")
		policyLabels := map[string]string{"policy": policyName}
		_, present, err := framework.MetricsSampleValue(before,
			"optipod_reconciliation_duration_seconds_count", policyLabels)
		Expect(err).NotTo(HaveOccurred(), "parse the pre-reconcile scrape")
		Expect(present).To(BeFalse(),
			"the metrics endpoint already carries a reconciliation counter for policy %q before it was created", policyName)
		monitoredLabels := map[string]string{"namespace": ns.Name, "policy": policyName}
		_, present, err = framework.MetricsSampleValue(before, "optipod_workloads_monitored", monitoredLabels)
		Expect(err).NotTo(HaveOccurred(), "parse the pre-reconcile scrape")
		Expect(present).To(BeFalse(),
			"the metrics endpoint already carries a monitored-workload series for policy %q before it was created", policyName)

		By("creating a named Recommend policy and waiting for a completed reconcile")
		createdAt := time.Now()
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeRecommend,
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the observability policy")
		serverCreation := policy.CreationTimestamp.Time
		annotated := waitForDeploymentAnnotation(ctx, ns.Name, workload.Name, recommendationAnnotations(samples))
		expectManagementAnnotations(annotated, policy, serverCreation)
		firstCompleted := waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})
		Expect(firstCompleted.Status.LastReconciliation).NotTo(BeNil(),
			"the completed reconcile recorded no LastReconciliation to anchor the further-reconcile wait on")
		// The workload must be untouched by the Recommend mode (no requests
		// change, no pod churn) so the metrics below reflect named processing,
		// not a side effect of an apply.
		expectUnappliedContainerRequests(annotated.Spec.Template.Spec.Containers, samples,
			"the workload under the observability Recommend policy")
		Expect(podUIDs(ctx, ns.Name, podSelector)).To(Equal(beforeUIDs),
			"the observability policy replaced workload pods")

		By("scraping the metrics endpoint after the completed named reconcile")
		after := expectScrapeSucceeds(ctx, scraper)
		Expect(framework.MetricsFamiliesPresent(after,
			"optipod_workloads_monitored",
			"optipod_reconciliation_duration_seconds",
			"optipod_metrics_collection_duration_seconds")).To(BeTrue(),
			"the completed reconcile of policy %q produced no OptiPod metric families; named processing is not exposed",
			policyName)
		monitored, present, err := framework.MetricsSampleValue(after, "optipod_workloads_monitored", monitoredLabels)
		Expect(err).NotTo(HaveOccurred(), "parse the post-reconcile scrape")
		Expect(present).To(BeTrue(),
			"the completed reconcile of policy %q produced no optipod_workloads_monitored series for namespace=%q policy=%q",
			policyName, ns.Name, policyName)
		Expect(monitored).To(Equal(1.0),
			"optipod_workloads_monitored{namespace=%q,policy=%q} = %v, want 1 (one discovered workload)",
			ns.Name, policyName, monitored)
		durationCount, present, err := framework.MetricsSampleValue(after,
			"optipod_reconciliation_duration_seconds_count", policyLabels)
		Expect(err).NotTo(HaveOccurred(), "parse the post-reconcile scrape")
		Expect(present).To(BeTrue(),
			"the completed reconcile of policy %q produced no optipod_reconciliation_duration_seconds_count series", policyName)
		Expect(durationCount).To(BeNumerically(">=", 1),
			"the named reconciliation counter is %v after a completed reconcile, want >= 1", durationCount)
		// The provider label on this metric is currently the fixed constant
		// "metrics-server" regardless of the configured provider
		// (workload_processor.go defaults metricsProviderType); the assertion
		// that matters here is that the named reconcile performed a metrics
		// collection at all, so the series presence is checked without
		// constraining that label value.
		_, queried, err := framework.MetricsSampleValue(after,
			"optipod_metrics_collection_duration_seconds_count", nil)
		Expect(err).NotTo(HaveOccurred(), "parse the post-reconcile scrape")
		Expect(queried).To(BeTrue(),
			"the named reconcile performed no metrics collection (optipod_metrics_collection_duration_seconds is absent)")

		By("waiting for a further completed reconcile of the same named policy")
		// Anchor on the FIRST completed reconcile's recorded timestamp, not the
		// policy creation time: creation already precedes the first reconcile,
		// so waiting "after creation" would return immediately and compare a
		// counter against itself.
		reconciled := waitForNextReconcile(ctx, ns.Name, policyName, firstCompleted.Status.LastReconciliation.Time)
		Expect(reconciled.Status.WorkloadsProcessed).To(Equal(1),
			"the further reconcile did not process the workload again")

		By("scraping again and asserting the named counter really changed")
		afterFurther := expectScrapeSucceeds(ctx, scraper)
		furtherCount, present, err := framework.MetricsSampleValue(afterFurther,
			"optipod_reconciliation_duration_seconds_count", policyLabels)
		Expect(err).NotTo(HaveOccurred(), "parse the post-further-reconcile scrape")
		Expect(present).To(BeTrue(),
			"the named reconciliation counter disappeared from the exposition after a further reconcile")
		Expect(furtherCount).To(BeNumerically(">", durationCount),
			"the named reconciliation counter did not change across a further completed reconcile (before %v, after %v); a static scrape is not processing evidence",
			durationCount, furtherCount)

		By("reading the deployed controller's structured log records for this policy")
		logs, err := controllerLogs(ctx)
		Expect(err).NotTo(HaveOccurred(), "read the deployed controller logs")
		processing := structuredRecordsAfter(logs, "Processing workload with selected policy", serverCreation)
		Expect(processing).NotTo(BeEmpty(),
			"no parseable structured record ties policy %q to workload processing after its creation", policyName)
		var processingRecord structuredLogRecord
		for _, record := range processing {
			if record.fieldsString("policy") == policyName {
				processingRecord = record
				break
			}
		}
		Expect(processingRecord.fields).NotTo(BeNil(),
			"no 'Processing workload with selected policy' record names policy %q in its JSON fields", policyName)
		Expect(processingRecord.fieldsString("workload")).To(Equal(ns.Name+"/"+workload.Name),
			"the structured record ties policy %q to the wrong workload identity: %v",
			policyName, processingRecord.fields["workload"])
		weight, weightErr := processingRecord.fieldsFloat("weight")
		Expect(weightErr).NotTo(HaveOccurred(), "the structured record's weight field is not a number: %v", processingRecord.fields["weight"])
		Expect(weight).To(Equal(100.0),
			"the structured record reports weight %v for the default-weight policy %q", weight, policyName)

		outcome := structuredRecordsAfter(logs, "Completed workload processing with policy selection", serverCreation)
		Expect(outcome).NotTo(BeEmpty(),
			"no parseable structured record reports the policy-selection outcome after policy %q was created", policyName)
		var outcomeRecord structuredLogRecord
		for _, record := range outcome {
			if record.fieldsString("policy") == policyName {
				outcomeRecord = record
				break
			}
		}
		Expect(outcomeRecord.fields).NotTo(BeNil(),
			"no 'Completed workload processing' record names policy %q in its JSON fields", policyName)
		discovered, err := outcomeRecord.fieldsFloat("discovered")
		Expect(err).NotTo(HaveOccurred(), "the outcome record's discovered field is not a number: %v", outcomeRecord.fields["discovered"])
		processed, err := outcomeRecord.fieldsFloat("processed")
		Expect(err).NotTo(HaveOccurred(), "the outcome record's processed field is not a number: %v", outcomeRecord.fields["processed"])
		Expect(discovered).To(Equal(1.0), "the outcome record reports %v discovered workloads, want the one matching workload", discovered)
		Expect(processed).To(Equal(1.0), "the outcome record reports %v processed workloads, want the one matching workload", processed)

		By("asserting the scrape and the matched log records carry no sensitive values")
		expectNoSensitiveValues(before, "the pre-reconcile metrics exposition")
		expectNoSensitiveValues(after, "the post-reconcile metrics exposition")
		for _, record := range []structuredLogRecord{processingRecord, outcomeRecord} {
			expectNoSensitiveValues(record.raw, fmt.Sprintf("the structured %q log record", record.message))
		}

		By("verifying the named policy and workload exist as API objects backing the evidence")
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: policyName},
			&optipodv1alpha1.OptimizationPolicy{})).To(Succeed(),
			"the policy named in the metrics series and log records is not API-observable")
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: workload.Name},
			annotated)).To(Succeed(),
			"the workload named in the structured log record is not API-observable")
	})
})

// scrapeUntilServing performs the first scrape through a bounded wait: a
// freshly installed or recently replaced controller can take a few seconds
// before its metrics Service has ready endpoints, and a scrape error during
// that window must not be mistaken for a broken metrics path. Every distinct
// scrape error observed along the way is preserved in the failure message so
// the real cause is never masked by a final timeout symptom.
func scrapeUntilServing(ctx context.Context, scraper *framework.ControllerMetricsScraper) string {
	var exposition string
	var seenErrors []string
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout:     60 * time.Second,
		Description: "the first metrics scrape through the shipped metrics service to succeed",
	}, func(ctx context.Context) (bool, error) {
		scraped, err := scraper.Scrape(ctx)
		if err != nil {
			msg := err.Error()
			if len(seenErrors) == 0 || seenErrors[len(seenErrors)-1] != msg {
				seenErrors = append(seenErrors, msg)
			}
			return false, err
		}
		exposition = scraped
		return true, nil
	})
	if err != nil {
		Fail(fmt.Sprintf("the supported metrics access path never served a scrape (in-cluster scrape of %s through the metrics fixture); distinct errors observed:\n  %s",
			framework.ControllerMetricsURL(), strings.Join(seenErrors, "\n  ")))
	}
	return exposition
}

// expectScrapeSucceeds is the plain scrape used after the path is proven
// serving: errors propagate so a mid-spec metrics failure cannot be silently
// retried away.
func expectScrapeSucceeds(ctx context.Context, scraper *framework.ControllerMetricsScraper) string {
	exposition, err := scraper.Scrape(ctx)
	Expect(err).NotTo(HaveOccurred(), "the metrics scrape failed after the path was proven serving")
	return exposition
}

// expectNoSensitiveValues fails when the given evidence text would be altered
// by the harness scrubber, i.e. when it contains sensitive values that must
// never be part of observability evidence.
func expectNoSensitiveValues(content, what string) {
	if strings.Contains(content, "Bearer ") || strings.Contains(content, "eyJ") {
		Fail(fmt.Sprintf("%s contains bearer-token material, which observability evidence must exclude", what))
	}
	_, redactions := framework.RedactCount(content)
	Expect(redactions).To(Equal(0),
		"%s required %d redactions, so it contained sensitive values", what, redactions)
}

// structuredLogRecord is one parsed console-encoded zap line of the deployed
// controller's log: the kubelet timestamps prefix, the zap line's level and
// message, and the trailing JSON fields object decoded into a map.
type structuredLogRecord struct {
	timestamp time.Time
	level     string
	message   string
	fields    map[string]any
	raw       string
}

// structuredRecordsAfter parses the controller log and returns the records
// with the given message whose kubelet timestamp is at or after the reference
// time. Only lines whose trailing fields section decodes as a JSON object
// count: a substring match on a message alone is not a parseable structured
// record.
func structuredRecordsAfter(logs, message string, after time.Time) []structuredLogRecord {
	var records []structuredLogRecord
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, message) {
			continue
		}
		record, ok := parseStructuredLogLine(line)
		if !ok {
			continue
		}
		if record.message != message {
			continue
		}
		if record.timestamp.Before(after.Add(-clockSkewTolerance)) {
			continue
		}
		records = append(records, record)
	}
	return records
}

// parseStructuredLogLine decodes one line of
// `<kubelet RFC3339Nano timestamp> <zap ISO8601 timestamp>\t<LEVEL>\t<message>\t{<JSON fields>}`.
func parseStructuredLogLine(line string) (structuredLogRecord, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return structuredLogRecord{}, false
	}
	kubeletTS, zapLine, found := strings.Cut(trimmed, " ")
	if !found {
		zapLine = trimmed
		kubeletTS = ""
	}
	timestamp := time.Time{}
	if kubeletTS != "" {
		parsed, err := time.Parse(time.RFC3339Nano, kubeletTS)
		if err != nil {
			return structuredLogRecord{}, false
		}
		timestamp = parsed
	}
	parts := strings.Split(zapLine, "\t")
	if len(parts) < 4 {
		return structuredLogRecord{}, false
	}
	level := parts[1]
	fieldsSection := parts[len(parts)-1]
	// The message is always the element immediately before the trailing fields
	// object; anything between the level and it (logger name, caller) would
	// only make the record more attributable, never less parseable.
	message := parts[len(parts)-2]
	if !strings.HasPrefix(fieldsSection, "{") || !strings.HasSuffix(fieldsSection, "}") {
		// Without a trailing JSON fields object the line is not a structured
		// record tying anything to anything.
		return structuredLogRecord{}, false
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(fieldsSection), &fields); err != nil {
		return structuredLogRecord{}, false
	}
	return structuredLogRecord{
		timestamp: timestamp,
		level:     level,
		message:   message,
		fields:    fields,
		raw:       trimmed,
	}, true
}

func (r structuredLogRecord) fieldsString(key string) string {
	if r.fields == nil {
		return ""
	}
	value, ok := r.fields[key].(string)
	if !ok {
		return ""
	}
	return value
}

func (r structuredLogRecord) fieldsFloat(key string) (float64, error) {
	if r.fields == nil {
		return 0, fmt.Errorf("record %q has no decoded fields object", r.message)
	}
	value, ok := r.fields[key]
	if !ok {
		return 0, fmt.Errorf("record %q has no %q field (fields: %v)", r.message, key, r.fields)
	}
	switch v := value.(type) {
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("record %q field %q is %T, not a number", r.message, key, value)
	}
}
