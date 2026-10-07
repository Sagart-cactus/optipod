package suite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/framework"
)

// The diagnostics self-check proves this feature's own machinery on the live
// owned cluster in the green path: a bundle collected while the cluster is
// still up is actionable (controller logs, events, object YAML/status, pod/
// deployment state, webhook configuration) and carries no canary-shaped
// sensitive content, with Secret values structurally excluded.
var _ = Describe("Harness diagnostics and reporting machinery", Label("diagnostics"), func() {
	It("collects an actionable redacted bundle from the live cluster", func(ctx context.Context) {
		ns := specNamespace(ctx, "diagnostics-selfcheck")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the self-check namespace")
		})

		workload := seedDiagnosticsCanary(ctx, ns.Name)

		By("waiting for the seeded workload to become Available")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workload.Name,
			framework.WaitOptions{Timeout: 3 * time.Minute})).To(Succeed(),
			"seeded workload never became Available")

		By("collecting a bundle while the owned cluster is up")
		bundleDir := filepath.Join(artifacts.Dir, "spec-diagnostics")
		bundle, err := framework.CollectDiagnostics(ctx, env.Clients, framework.DiagnosticsScope{
			Namespaces:     appendUnique([]string{ns.Name}, framework.SystemNamespace, framework.FixtureNamespace),
			FailureSummary: "self-check (no failure): verifying the collector's actionable output and redaction",
			ClusterName:    env.Cluster.Name,
			Pins:           framework.PinManifest(),
		}, bundleDir)
		Expect(err).NotTo(HaveOccurred(), "diagnostics collection failed")
		Expect(bundle.CollectionErrors).To(BeEmpty(),
			"unexpected diagnostic collection errors: %s", strings.Join(bundle.CollectionErrors, "\n"))

		By("asserting the bundle covers controller logs, events, objects, and webhook configuration")
		files := bundleFiles(bundle)
		Expect(files).To(ContainElement(filepath.Join("system", "controller-deployment.yaml")),
			"bundle lacks controller deployment state")
		Expect(files).To(ContainElement(filepath.Join("system", "webhook-configuration.yaml")),
			"bundle lacks webhook configuration")
		Expect(files).To(ContainElement(filepath.Join("namespaces", ns.Name, "deployments.yaml")),
			"bundle lacks the seeded workload YAML/status")
		Expect(files).To(ContainElement(filepath.Join("namespaces", ns.Name, "pods.yaml")),
			"bundle lacks pod state")
		Expect(files).To(ContainElement(filepath.Join("namespaces", ns.Name, "events.json")),
			"bundle lacks scoped events")
		Expect(files).To(ContainElement(filepath.Join("namespaces", ns.Name, "optimizationpolicies.yaml")),
			"bundle lacks policy YAML/status")

		webhookConfig := readBundleFile(bundleDir, filepath.Join("system", "webhook-configuration.yaml"))
		Expect(webhookConfig).To(ContainSubstring("failurePolicy: Ignore"))
		Expect(webhookConfig).To(ContainSubstring("path: /mutate"))
		Expect(webhookConfig).To(ContainSubstring("optipod-e2e-webhook-ca"),
			"CA metadata missing from webhook configuration")
		Expect(webhookConfig).NotTo(ContainSubstring("BEGIN CERTIFICATE"),
			"raw CA bundle bytes leaked into webhook configuration (metadata only)")

		deployments := readBundleFile(bundleDir, filepath.Join("namespaces", ns.Name, "deployments.yaml"))
		Expect(deployments).To(ContainSubstring(workload.Name), "workload YAML missing its name")
		Expect(deployments).To(ContainSubstring("name: app"), "workload YAML missing container state")
		Expect(deployments).To(ContainSubstring("Available"),
			"workload YAML missing observed conditions")

		controllerLogs := anyFileMatching(bundleDir, "system/controller-logs/")
		Expect(controllerLogs).NotTo(BeEmpty(), "no controller logs were collected")
		for _, path := range controllerLogs {
			Expect(readBundleFile(bundleDir, path)).NotTo(BeEmpty(), "controller log %s is empty", path)
		}

		By("asserting content-level redaction removed the canary credentials from the collected text")
		Expect(deployments).To(ContainSubstring("e2e-harness.optipod.io/canary-token: [REDACTED]"),
			"canary bearer token annotation was not redacted in the collected workload YAML")
		Expect(deployments).To(ContainSubstring(redactedPEMShape("EC")),
			"canary private-key annotation was not redacted in the collected workload YAML")
		Expect(bundle.Redactions).To(BeNumerically(">", 0), "no redactions were applied to the bundle")

		By("running the negative sensitive-data scan over every produced file")
		for _, file := range bundle.Files {
			content := readBundleFile(bundleDir, file)
			Expect(content).NotTo(ContainSubstring(canaryTokenBody),
				"canary bearer token body survived in %s", file)
			Expect(content).NotTo(ContainSubstring(canaryPEMBody),
				"canary private-key material survived in %s", file)
		}

		By("asserting Secret values were structurally excluded (metadata only)")
		secretsMeta := readBundleFile(bundleDir, filepath.Join("namespaces", ns.Name, "secrets-metadata.json"))
		Expect(secretsMeta).To(ContainSubstring(canarySecretName),
			"Secret metadata missing the canary secret (presence should be recorded)")
		Expect(secretsMeta).To(ContainSubstring("tls.key"), "Secret metadata missing key names")
		Expect(secretsMeta).NotTo(ContainSubstring(canaryTokenBody),
			"canary Secret value leaked through Secret metadata")
	})
})

func bundleFiles(bundle *framework.DiagnosticBundle) []string {
	out := make([]string, 0, len(bundle.Files))
	for _, f := range bundle.Files {
		if f == "summary.json" || f == "failures.txt" || f == "collection-errors.txt" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func readBundleFile(bundleDir, rel string) string {
	data, err := os.ReadFile(filepath.Join(bundleDir, rel))
	Expect(err).NotTo(HaveOccurred(), "read bundle file %s", rel)
	return string(data)
}

// anyFileMatching returns the bundle-relative paths that contain the prefix.
func anyFileMatching(bundleDir, prefix string) []string {
	entries, err := os.ReadDir(filepath.Join(bundleDir, filepath.FromSlash(prefix)))
	Expect(err).NotTo(HaveOccurred(), "list %s", prefix)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join(filepath.FromSlash(prefix), e.Name())))
	}
	return out
}

// redactedPEMShape is the scrubber's output shape for a redacted PEM block of
// the given kind: fences preserved, body replaced by the placeholder. It is
// assembled at runtime from fence constants, like canaryPEM, so no literal
// PEM-shaped block exists in source.
func redactedPEMShape(kind string) string {
	return "-----BEGIN " + kind + " PRIVATE KEY-----" + framework.RedactPlaceholder +
		"-----END " + kind + " PRIVATE KEY-----"
}
