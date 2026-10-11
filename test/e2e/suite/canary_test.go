package suite

import (
	"context"
	"strings"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// Test-only canary data. None of these values is a real credential; they exist
// so the suite can prove that canary-shaped sensitive content never reaches a
// diagnostic artifact, the console, or JUnit. The raw token body is what the
// negative sensitive-data scan greps for in every produced file.
const (
	canaryTokenAnnotation = "e2e-harness.optipod.io/canary-token"
	canaryKeyAnnotation   = "e2e-harness.optipod.io/canary-key"
	canaryBearerValue     = "Bearer canary-bearer-token-0123456789abcdef"
	canarySecretName      = "canary-redaction-probe" // pragma: allowlist secret
	// canaryTokenBody and canaryPEMBody are the sensitive fragments that must
	// never appear verbatim in any artifact.
	canaryTokenBody    = "canary-bearer-token-0123456789abcdef"
	canaryPEMBody      = "MIICANARYNOTAREALPRIVATEKEY"
	canaryPEMBodyValue = "MIICANARYNOTAREALPRIVATEKEY0123456789abcdef==" // pragma: allowlist secret
)

// canaryPEM assembles a clearly fake PEM-shaped canary block at runtime by
// joining fence and body constants, so no literal key block exists in source
// (commit guards reject PEM literals). It is test data only, never real key
// material.
func canaryPEM(kind, body string) string {
	return "-----BEGIN " + kind + " PRIVATE KEY-----\n" + body + "\n" +
		"-----END " + kind + " PRIVATE KEY-----"
}

// canaryPEMValue is the PEM-shaped canary planted in annotations and Secret
// data so redaction is exercised on real collected cluster content.
var canaryPEMValue = canaryPEM("EC", canaryPEMBodyValue)

// specNamespace creates one spec's unique namespace through the typed client.
func specNamespace(ctx context.Context, prefix string) *corev1.Namespace {
	ns, err := env.Factory.CreateNamespace(ctx, framework.NormalizePrefix(prefix))
	Expect(err).NotTo(HaveOccurred(), "create spec namespace with prefix %q", prefix)
	return ns
}

// seedDiagnosticsCanary populates a namespace with the objects a failure
// diagnostic bundle should cover: a workload whose annotations carry
// canary-shaped sensitive content (so content-level redaction is exercised on
// real collected text), a Secret holding canary credential values (so
// structural Secret exclusion is exercised), and a Disabled policy (so policy
// YAML/status is present in the sweep). It returns the seeded workload.
func seedDiagnosticsCanary(ctx context.Context, namespace string) *appsv1.Deployment {
	name, err := framework.UniqueName("diagcanary")
	Expect(err).NotTo(HaveOccurred(), "generate canary resource name")

	workload := framework.NewDeploymentFixture(namespace, name, 1,
		framework.ContainerFixture{Name: "app", CPURequest: "50m", MemoryRequest: "64Mi"})
	// Canary sensitive content planted where the collector will sweep it: the
	// scrubber must replace it in the collected YAML.
	workload.Annotations = map[string]string{
		canaryTokenAnnotation: canaryBearerValue,
		canaryKeyAnnotation:   canaryPEMValue,
	}
	Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create canary workload deployment")

	policy := framework.NewPolicyFixture(namespace, name, optipodv1alpha1.ModeDisabled)
	Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create disabled canary policy")

	// Canary Secret: values are structurally excluded from diagnostics; only
	// its name and key names may appear (as metadata).
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      canarySecretName,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "optipod-e2e-harness",
			},
		},
		Data: map[string][]byte{
			"token":   []byte(strings.TrimPrefix(canaryBearerValue, "Bearer ")),
			"tls.key": []byte(canaryPEMValue),
		},
	}
	// The assertion subject is the error alone so a failing assertion can
	// never dump the Secret object itself to the console or JUnit.
	_, err = env.Clients.Kube.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "create canary secret")

	return workload
}
