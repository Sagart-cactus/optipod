package framework

import (
	"bytes"
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultAwaitTimeout = 5 * time.Minute

// InstallOptions configures installation into the owned cluster.
type InstallOptions struct {
	// KubectlPath overrides the kubectl binary; defaults to PATH lookup.
	KubectlPath string
	// Run overrides command execution (tests).
	Run commandRunner
	// AwaitTimeout bounds the readiness wait; defaults to 5 minutes.
	AwaitTimeout time.Duration
}

// Install applies the rendered manifest exactly once with the private
// kubeconfig, then creates the webhook serving-certificate Secret through the
// typed client so the private key never touches the filesystem.
func Install(ctx context.Context, cluster *Cluster, clients *Clients,
	rendered *RenderedManifests, webhookCert *WebhookServingCert, opts InstallOptions) error {

	kubectl := opts.KubectlPath
	if kubectl == "" {
		kubectl = "kubectl"
	}
	run := opts.Run
	if run == nil {
		run = execRunner
	}

	applyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := run(applyCtx, kubectl, "--kubeconfig", cluster.Kubeconfig,
		"apply", "-f", rendered.Path)
	if err != nil {
		return fmt.Errorf("apply rendered manifest %s: %w (%s)", rendered.Path, err, tailOutput(string(out)))
	}

	if err := ensureWebhookCertSecret(ctx, clients, webhookCert); err != nil {
		return err
	}
	return nil
}

// ensureWebhookCertSecret creates (or replaces) the TLS Secret the shipped
// manager webhook patch mounts at /tmp/k8s-webhook-server/serving-certs.
func ensureWebhookCertSecret(ctx context.Context, clients *Clients, cert *WebhookServingCert) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      WebhookCertSecretName,
			Namespace: SystemNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "optipod",
				"app.kubernetes.io/managed-by": "optipod-e2e-harness",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": cert.CertPEM,
			"tls.key": cert.KeyPEM,
		},
	}
	_, err := clients.Kube.CoreV1().Secrets(SystemNamespace).Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		_, err = clients.Kube.CoreV1().Secrets(SystemNamespace).Update(ctx, secret, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("ensure webhook cert secret %s/%s: %w", SystemNamespace, WebhookCertSecretName, err)
	}
	return nil
}

// readyStep is one bounded readiness gate.
type readyStep struct {
	desc string
	wait func(context.Context) error
}

// runReadySteps executes the gates in order with one shared bounded context
// so the total readiness wait cannot exceed the timeout.
func runReadySteps(ctx context.Context, timeout time.Duration, steps []readyStep) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for _, step := range steps {
		if err := step.wait(cctx); err != nil {
			return fmt.Errorf("await ready: %s: %w", step.desc, err)
		}
	}
	return nil
}

// AwaitReady establishes, with bounded condition waits, that the installed
// control plane is genuinely ready before any admission or policy scenario
// runs: the OptimizationPolicy CRD is Established, the controller and fixture
// Deployments are Available, the webhook Service has ready endpoints, and the
// MutatingWebhookConfiguration carries the harness CA bundle. A rendered
// manifest or a merely running pod is not accepted as readiness.
func AwaitReady(ctx context.Context, clients *Clients, webhookCert *WebhookServingCert, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultAwaitTimeout
	}
	steps := []readyStep{
		{"CRD " + OptimizationPolicyCRDName + " Established", func(ctx context.Context) error {
			return WaitForCRDEstablished(ctx, clients, OptimizationPolicyCRDName, WaitOptions{Timeout: timeout})
		}},
		{"controller Deployment Available", func(ctx context.Context) error {
			return WaitForDeploymentAvailable(ctx, clients, SystemNamespace, ControllerDeploymentName, WaitOptions{Timeout: timeout})
		}},
		{"metrics fixture Deployment Available", func(ctx context.Context) error {
			return WaitForDeploymentAvailable(ctx, clients, FixtureNamespace, FixtureServiceName, WaitOptions{Timeout: timeout})
		}},
		{"webhook Service endpoints", func(ctx context.Context) error {
			return WaitForServiceEndpoints(ctx, clients, SystemNamespace, WebhookServiceName, WaitOptions{Timeout: timeout})
		}},
		{"webhook CA bundle installed", func(ctx context.Context) error {
			return WaitForWebhookCABundle(ctx, clients, webhookCert.CAPEM, WaitOptions{Timeout: timeout})
		}},
	}
	return runReadySteps(ctx, timeout, steps)
}

// AwaitInstalledReady verifies that a retained harness cluster's installed
// pieces are genuinely ready before a focused reuse rerun runs its specs:
// the OptimizationPolicy CRD is Established, the controller and fixture
// Deployments are Available, and the webhook Service has ready endpoints. A
// focused rerun attaches to the retaining run's installation, so two of that
// run's checks are deliberately absent here: the webhook CA-bundle equality
// check (the CA was generated inside the retaining run's memory and the
// bundle the API server already trusts is the one the installed webhook
// serves) and the freshly built image identity check (a focused rerun does
// not rebuild the controller, so the installed image is the retaining run's
// build by design).
func AwaitInstalledReady(ctx context.Context, clients *Clients, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultAwaitTimeout
	}
	steps := []readyStep{
		{"CRD " + OptimizationPolicyCRDName + " Established", func(ctx context.Context) error {
			return WaitForCRDEstablished(ctx, clients, OptimizationPolicyCRDName, WaitOptions{Timeout: timeout})
		}},
		{"controller Deployment Available", func(ctx context.Context) error {
			return WaitForDeploymentAvailable(ctx, clients, SystemNamespace, ControllerDeploymentName, WaitOptions{Timeout: timeout})
		}},
		{"metrics fixture Deployment Available", func(ctx context.Context) error {
			return WaitForDeploymentAvailable(ctx, clients, FixtureNamespace, FixtureServiceName, WaitOptions{Timeout: timeout})
		}},
		{"webhook Service endpoints", func(ctx context.Context) error {
			return WaitForServiceEndpoints(ctx, clients, SystemNamespace, WebhookServiceName, WaitOptions{Timeout: timeout})
		}},
	}
	return runReadySteps(ctx, timeout, steps)
}

// WaitForWebhookCABundle waits until the MutatingWebhookConfiguration exists
// and carries the expected CA bundle for the pod-resource-mutator webhook.
func WaitForWebhookCABundle(ctx context.Context, clients *Clients, caPEM []byte, opts WaitOptions) error {
	if opts.Description == "" {
		opts.Description = "MutatingWebhookConfiguration " + WebhookConfigurationName + " caBundle"
	}
	return WaitForCondition(ctx, opts, func(ctx context.Context) (bool, error) {
		mwc, err := clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().
			Get(ctx, WebhookConfigurationName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, wh := range mwc.Webhooks {
			if wh.Name == PodMutatorWebhookName {
				return bytes.Equal(wh.ClientConfig.CABundle, caPEM), nil
			}
		}
		return false, nil
	})
}

// VerifyControllerIdentity proves the running controller pod is the image this
// run built and loaded (by normalized image config digest and exact reference)
// and runs under the shipped service account. This is the deployed-behavior
// evidence that the install used the checkout's controller and shipped RBAC
// identity rather than any pre-existing or cached image.
func VerifyControllerIdentity(ctx context.Context, clients *Clients, image *BuiltImage) error {
	pods, err := clients.Kube.CoreV1().Pods(SystemNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: ControllerPodSelector,
	})
	if err != nil {
		return fmt.Errorf("list controller pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no controller-manager pods found in %s", SystemNamespace)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Spec.ServiceAccountName != ControllerServiceAccountName {
			return fmt.Errorf("controller pod %s uses service account %q, want shipped %q",
				pod.Name, pod.Spec.ServiceAccountName, ControllerServiceAccountName)
		}
		var specImage, statusImageID string
		for _, c := range pod.Spec.Containers {
			if c.Name == ControllerContainerName {
				specImage = c.Image
			}
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name == ControllerContainerName {
				statusImageID = cs.ImageID
			}
		}
		if specImage != image.Ref {
			return fmt.Errorf("controller pod %s image %q, want freshly built %q", pod.Name, specImage, image.Ref)
		}
		// A pod's imageID reports the node containerd digest (NodeID when the
		// image was loaded by this run); fall back to the local daemon ID.
		wantDigest := image.NodeID
		if wantDigest == "" {
			wantDigest = image.ID
		}
		if got := NormalizeImageDigest(statusImageID); got != wantDigest {
			return fmt.Errorf("controller pod %s imageID %q (digest %q), want built/loaded image digest %q",
				pod.Name, statusImageID, got, wantDigest)
		}
	}
	return nil
}
