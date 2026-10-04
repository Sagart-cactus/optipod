package framework

import (
	"context"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

const testImageDigest = "sha256:4613778f3cfcd10e615029370f5786704559103cf27bef934597ba562b269661"

// TestEnsureWebhookCertSecretCreatesAndUpdates verifies the TLS secret carrying
// the serving cert/key is created through the typed client and replaced on
// conflict, so the private key never touches the filesystem.
func TestEnsureWebhookCertSecretCreatesAndUpdates(t *testing.T) {
	cert, err := GenerateWebhookServingCert(WebhookServiceName, SystemNamespace)
	if err != nil {
		t.Fatal(err)
	}
	kube := fake.NewSimpleClientset()
	clients := &Clients{Kube: kube}

	if err := ensureWebhookCertSecret(context.Background(), clients, cert); err != nil {
		t.Fatalf("create: %v", err)
	}
	secret, err := kube.CoreV1().Secrets(SystemNamespace).Get(context.Background(), WebhookCertSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if secret.Type != corev1.SecretTypeTLS {
		t.Errorf("secret type = %q", secret.Type)
	}
	if string(secret.Data["tls.crt"]) != string(cert.CertPEM) ||
		string(secret.Data["tls.key"]) != string(cert.KeyPEM) {
		t.Error("secret data does not match generated key pair")
	}

	// Re-install (idempotent) replaces rather than fails.
	if err := ensureWebhookCertSecret(context.Background(), clients, cert); err != nil {
		t.Fatalf("update on conflict: %v", err)
	}
}

func controllerPod(name, image, imageID, sa string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: SystemNamespace,
			Labels:    map[string]string{"control-plane": "controller-manager"},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: sa,
			Containers:         []corev1.Container{{Name: "manager", Image: image}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{Name: "manager", ImageID: imageID}},
		},
	}
}

func TestVerifyControllerIdentity(t *testing.T) {
	built := &BuiltImage{Ref: "optipod-e2e/controller:e2e-1-abcd", ID: testImageDigest}

	tests := []struct {
		name    string
		image   *BuiltImage
		pods    []corev1.Pod
		wantErr string
	}{
		{
			name: "matching image ID, ref and shipped SA passes",
			pods: []corev1.Pod{*controllerPod("optipod-controller-manager-x", built.Ref,
				"docker.io/library/"+built.Ref+"@"+testImageDigest, ControllerServiceAccountName)},
		},
		{
			name: "node store digest is preferred when the run loaded the image",
			image: &BuiltImage{Ref: built.Ref, ID: testImageDigest,
				NodeID: "sha256:33711cdf65017ad4a89c2ec443ee212f767fcdb2eaf33c1244c47f3e9bbef350"},
			pods: []corev1.Pod{*controllerPod("optipod-controller-manager-x", built.Ref,
				"sha256:33711cdf65017ad4a89c2ec443ee212f767fcdb2eaf33c1244c47f3e9bbef350", ControllerServiceAccountName)},
		},
		{
			name:    "no pods fails",
			pods:    nil,
			wantErr: "no controller-manager pods",
		},
		{
			name: "stale image ID fails",
			pods: []corev1.Pod{*controllerPod("p1", built.Ref,
				"docker.io/library/optipod-e2e/controller@sha256:0000000000000000000000000000000000000000000000000000000000000000",
				ControllerServiceAccountName)},
			wantErr: "imageID",
		},
		{
			name:    "wrong image reference fails",
			pods:    []corev1.Pod{*controllerPod("p1", "controller:latest", testImageDigest, ControllerServiceAccountName)},
			wantErr: "want freshly built",
		},
		{
			name:    "wrong service account fails",
			pods:    []corev1.Pod{*controllerPod("p1", built.Ref, testImageDigest, "default")},
			wantErr: "service account",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := make([]runtime.Object, 0, len(tc.pods))
			for i := range tc.pods {
				objs = append(objs, &tc.pods[i])
			}
			clients := &Clients{Kube: fake.NewSimpleClientset(objs...)}
			image := tc.image
			if image == nil {
				image = built
			}
			err := VerifyControllerIdentity(context.Background(), clients, image)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestWaitForWebhookCABundleMatchesInstalledCA(t *testing.T) {
	cert, err := GenerateWebhookServingCert(WebhookServiceName, SystemNamespace)
	if err != nil {
		t.Fatal(err)
	}
	mwc := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: WebhookConfigurationName},
		Webhooks: []admissionv1.MutatingWebhook{{
			Name:         "pod-resource-mutator.optipod.io",
			ClientConfig: admissionv1.WebhookClientConfig{CABundle: cert.CAPEM},
		}},
	}
	clients := &Clients{Kube: fake.NewSimpleClientset(mwc)}
	err = WaitForWebhookCABundle(context.Background(), clients, cert.CAPEM,
		WaitOptions{Timeout: 2 * time.Second, Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	other, err := GenerateWebhookServingCert(WebhookServiceName, SystemNamespace)
	if err != nil {
		t.Fatal(err)
	}
	err = WaitForWebhookCABundle(context.Background(), clients, other.CAPEM,
		WaitOptions{Timeout: 100 * time.Millisecond, Interval: 10 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "caBundle") {
		t.Fatalf("expected mismatch timeout naming the caBundle wait, got %v", err)
	}
}
