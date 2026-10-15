package suite

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// This scenario proves the installed mutating webhook's TLS, readiness and
// configured failure-policy behavior through real API admission (VAL-CONF-012).
// It never trusts static YAML alone:
//
//   - It reads the installed MutatingWebhookConfiguration and requires the
//     production service reference, admission path, object selector and
//     failurePolicy=Ignore, then proves the webhook Service has a ready
//     endpoint backed by a Ready controller pod.
//   - It verifies the serving certificate the endpoint presents: it must chain
//     to the INSTALLED caBundle with the webhook Service DNS name as the
//     verified name and carry the Service SANs. Only the public certificate is
//     read (from the installed TLS Secret); the private key is never accessed.
//     The API server's own verification of that exact certificate is then
//     proven by a real admission mutation, which failurePolicy=Ignore would
//     otherwise not produce if TLS verification had failed.
//   - It creates an eligible pod through the real API and observes the
//     admission mutation.
//   - It proves the failure-policy behavior with real API outcomes: a scoped
//     Fail configuration (unique configuration and label, unreachable
//     service) rejects a matching pod; removing it restores admission. During
//     a scoped outage of the installed webhook server the installed
//     failurePolicy=Ignore admits an eligible pod UNMUTATED, and once the
//     server is restored a fresh eligible pod is mutated again.
var _ = Describe("Webhook TLS, readiness and configured failure policy", Label("conformance", "webhook"), func() {
	It("verifies the served certificate and endpoint, then proves Ignore admits unmutated during an outage and Fail rejects, with mutation restored", func(ctx context.Context) {
		ns := specNamespace(ctx, "conf-webhook-tls")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the webhook TLS conformance spec namespace")
		})

		By("reading the installed MutatingWebhookConfiguration and requiring the shipped shape")
		webhook := installedPodMutator(ctx)
		Expect(webhook.ClientConfig.Service).NotTo(BeNil(),
			"the installed mutating webhook does not target an in-cluster Service")
		Expect(webhook.ClientConfig.Service.Name).To(Equal(framework.WebhookServiceName),
			"the installed webhook targets service %q, want the shipped %q",
			webhook.ClientConfig.Service.Name, framework.WebhookServiceName)
		Expect(webhook.ClientConfig.Service.Namespace).To(Equal(framework.SystemNamespace),
			"the installed webhook targets namespace %q, want the shipped %q",
			webhook.ClientConfig.Service.Namespace, framework.SystemNamespace)
		Expect(webhook.ClientConfig.Service.Port).NotTo(BeNil(),
			"the installed webhook service reference has no port")
		Expect(*webhook.ClientConfig.Service.Port).To(Equal(int32(framework.WebhookServicePort)),
			"the installed webhook targets service port %d, want the shipped %d",
			*webhook.ClientConfig.Service.Port, framework.WebhookServicePort)
		Expect(webhook.ClientConfig.Service.Path).NotTo(BeNil(),
			"the installed webhook targets no admission path")
		Expect(*webhook.ClientConfig.Service.Path).To(Equal(framework.WebhookPath),
			"the installed webhook targets %q, want the production %q admission path",
			*webhook.ClientConfig.Service.Path, framework.WebhookPath)
		Expect(webhook.ClientConfig.CABundle).NotTo(BeEmpty(),
			"the installed webhook has no caBundle, so the API server could not verify the serving certificate")
		Expect(webhook.FailurePolicy).NotTo(BeNil(), "the installed webhook has no failure policy")
		Expect(*webhook.FailurePolicy).To(Equal(admissionregv1.Ignore),
			"the installed webhook must keep the shipped failurePolicy=Ignore")
		Expect(webhook.ObjectSelector).NotTo(BeNil(), "the installed webhook has no object selector")
		enablingLabelRequired := false
		for _, req := range webhook.ObjectSelector.MatchExpressions {
			if req.Key == optipodv1alpha1.AnnotationWebhookEnabled &&
				req.Operator == metav1.LabelSelectorOpIn &&
				len(req.Values) == 1 && req.Values[0] == "true" {
				enablingLabelRequired = true
			}
		}
		Expect(enablingLabelRequired).To(BeTrue(),
			"the installed webhook object selector does not require the supported optipod.io/webhook-enabled=true pod label")

		By("requiring the webhook Service endpoint to be backed by a Ready controller pod")
		endpointPod := webhookEndpointPod(ctx)
		Expect(endpointPod.Status.PodIP).NotTo(BeEmpty(), "the matched webhook endpoint pod has no IP")

		By("verifying the served certificate chains to the INSTALLED caBundle and carries the Service SANs")
		served := readInstalledServingCert(ctx, webhook.ClientConfig.CABundle)
		for _, dns := range framework.WebhookDNSNames(framework.WebhookServiceName, framework.SystemNamespace) {
			Expect(served.DNSNames).To(ContainElement(dns),
				"the webhook serving certificate lacks the Service DNS SAN %q (has %v)", dns, served.DNSNames)
		}
		now := time.Now()
		Expect(now.After(served.NotBefore)).To(BeTrue(),
			"the webhook serving certificate is not yet valid (NotBefore %s)", served.NotBefore)
		Expect(now.Before(served.NotAfter)).To(BeTrue(),
			"the webhook serving certificate is expired (NotAfter %s)", served.NotAfter)
		AddReportEntry("webhook-serving-cert", fmt.Sprintf(
			"subject=%s issuer=%s dnsNames=%v notBefore=%s notAfter=%s",
			served.Subject.CommonName, served.Issuer.CommonName, served.DNSNames,
			served.NotBefore.Format(time.RFC3339), served.NotAfter.Format(time.RFC3339)))

		By("creating a webhook-strategy Auto policy that selects the probe namespace")
		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique probe policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("webhook", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(),
			"create the webhook-strategy Auto policy")
		// The deployed mutator lists policies through the manager's shared cache,
		// so the policy's observed reconcile guarantees it is visible before any
		// probe is admitted.
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      0,
			processed:       0,
			reconciledAfter: policy.CreationTimestamp.Time,
		})

		By("proving live admission mutation succeeds (the API server trusted the served CA/TLS)")
		admittedName, err := framework.UniqueName("probe-admitted")
		Expect(err).NotTo(HaveOccurred(), "generate unique admitted probe name")
		admitted, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx,
			newAdmissionProbePod(ns.Name, admittedName, true, probeRecommendationAnnotations(true)),
			metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "the real API must admit the eligible probe pod")
		expectMutatedProbeRequests(admitted, "the admission response of the eligible probe")

		By("creating a scoped Fail webhook configuration that points at an unreachable service")
		failConfigName, err := framework.UniqueName("e2e-fail-config")
		Expect(err).NotTo(HaveOccurred(), "generate unique Fail configuration name")
		failWebhookName := failConfigName + ".optipod-e2e.test"
		unreachableService, err := framework.UniqueName("e2e-unreachable-webhook")
		Expect(err).NotTo(HaveOccurred(), "generate unique unreachable service name")
		failLabelValue, err := framework.UniqueName("fail")
		Expect(err).NotTo(HaveOccurred(), "generate unique Fail-probe label value")
		failConfig := &admissionregv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: failConfigName},
			Webhooks: []admissionregv1.MutatingWebhook{{
				Name: failWebhookName,
				ClientConfig: admissionregv1.WebhookClientConfig{
					Service: &admissionregv1.ServiceReference{
						Name:      unreachableService,
						Namespace: framework.SystemNamespace,
						Path:      stringPtr(framework.WebhookPath),
						Port:      int32Ptr(int32(framework.WebhookServicePort)),
					},
				},
				Rules: []admissionregv1.RuleWithOperations{{
					Operations: []admissionregv1.OperationType{admissionregv1.Create},
					Rule: admissionregv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods"},
					},
				}},
				ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					failProbeLabelKey: failLabelValue,
				}},
				FailurePolicy:           failurePolicyPtr(admissionregv1.Fail),
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             sideEffectsPtr(admissionregv1.SideEffectClassNone),
				TimeoutSeconds:          int32Ptr(5),
			}},
		}
		_, err = env.Clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().
			Create(ctx, failConfig, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "create the scoped Fail webhook configuration")
		DeferCleanup(func(context.Context) {
			// Safety net: a leftover Fail configuration would break unrelated
			// admission, so remove it even if the spec failed.
			cctx, cancel := framework.CleanupContext()
			defer cancel()
			delErr := env.Clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().
				Delete(cctx, failConfigName, metav1.DeleteOptions{})
			if delErr != nil && !apierrors.IsNotFound(delErr) {
				GinkgoWriter.Printf("cleanup: deleting scoped Fail webhook configuration %s failed: %v\n",
					failConfigName, delErr)
			}
		})

		By("asserting the scoped Fail configuration rejects a matching pod through real API admission")
		failPod, err := framework.UniqueName("fail-probe-pod")
		Expect(err).NotTo(HaveOccurred(), "generate unique Fail probe pod name")
		_, err = env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx,
			newFailProbePod(ns.Name, failPod, failLabelValue), metav1.CreateOptions{})
		Expect(err).To(HaveOccurred(),
			"a pod matching the scoped Fail configuration must be rejected when its webhook is unreachable")
		Expect(apierrors.IsInternalError(err)).To(BeTrue(),
			"the Fail-configuration rejection returned %v; want an admission webhook call failure (InternalError)", err)
		Expect(err.Error()).To(ContainSubstring(failWebhookName),
			"the Fail-configuration rejection error %q does not name the scoped webhook %q", err, failWebhookName)

		By("removing the scoped Fail configuration and observing admission restored")
		Expect(env.Clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().
			Delete(ctx, failConfigName, metav1.DeleteOptions{})).To(Succeed(),
			"delete the scoped Fail webhook configuration")
		restoredName, err := framework.UniqueName("fail-probe-restored")
		Expect(err).NotTo(HaveOccurred(), "generate unique restored probe pod name")
		_, err = env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx,
			newFailProbePod(ns.Name, restoredName, failLabelValue), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(),
			"after removing the scoped Fail configuration a matching pod must be admitted again")

		By("creating a scoped outage of the installed webhook server")
		expectWebhookServerOutage(ctx)
		DeferCleanup(func(context.Context) {
			// Safety net: always restore the shared webhook server.
			cctx, cancel := framework.CleanupContext()
			defer cancel()
			restoreWebhookServer(cctx)
		})

		By("asserting the installed failurePolicy=Ignore admits an eligible pod UNMUTATED during the outage")
		outageName, err := framework.UniqueName("probe-outage")
		Expect(err).NotTo(HaveOccurred(), "generate unique outage probe name")
		outageAdmitted, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx,
			newAdmissionProbePod(ns.Name, outageName, true, probeRecommendationAnnotations(true)),
			metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(),
			"failurePolicy=Ignore must admit the eligible pod even though the webhook call failed")
		expectUnmutatedProbeRequests(outageAdmitted, "the outage-admitted eligible pod")
		persistedOutage := &corev1.Pod{}
		Expect(env.Clients.Ctrl.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: outageName}, persistedOutage)).To(Succeed(),
			"read the persisted outage-admitted pod")
		expectUnmutatedProbeRequests(persistedOutage, "the persisted outage-admitted eligible pod")

		By("restoring the installed webhook server and observing mutation return")
		restoreWebhookServer(ctx)
		// A freshly created policy forces a status write (its reconciliation
		// summary starts empty), so waiting for that observed reconcile proves
		// the restarted controller's policy informer/cache is live before the
		// recovery probe relies on it. Waiting on the pre-existing policy would
		// be gated by the controller's one-minute summary refresh.
		recoveryPolicyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique recovery policy name")
		recoveryPolicy := framework.NewPolicyFixture(ns.Name, recoveryPolicyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("webhook", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, recoveryPolicy)).To(Succeed(),
			"create the post-restart recovery policy")
		waitForPolicyStatus(ctx, ns.Name, recoveryPolicyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      0,
			processed:       0,
			reconciledAfter: recoveryPolicy.CreationTimestamp.Time,
		})
		recoveredName, err := framework.UniqueName("probe-recovered")
		Expect(err).NotTo(HaveOccurred(), "generate unique recovered probe name")
		recovered, err := env.Clients.Kube.CoreV1().Pods(ns.Name).Create(ctx,
			newAdmissionProbePod(ns.Name, recoveredName, true, probeRecommendationAnnotations(true)),
			metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "the real API must admit the recovery probe pod")
		expectMutatedProbeRequests(recovered, "the recovery probe pod after the server returned")

		By("asserting the installed webhook configuration was never mutated by this scenario")
		finalWebhook := installedPodMutator(ctx)
		Expect(finalWebhook.FailurePolicy).NotTo(BeNil(), "the installed webhook lost its failure policy")
		Expect(*finalWebhook.FailurePolicy).To(Equal(admissionregv1.Ignore),
			"the installed webhook failurePolicy changed during the scenario")
		Expect(finalWebhook.ClientConfig.Service).NotTo(BeNil(),
			"the installed webhook lost its service reference during the scenario")
		Expect(finalWebhook.ClientConfig.Service.Name).To(Equal(framework.WebhookServiceName),
			"the installed webhook service reference changed during the scenario")
	})
})

// failProbeLabelKey is the unique object-selector key the scoped Fail
// configuration matches, distinct from the installed webhook's optipod.io
// enabling label so the two never overlap.
const failProbeLabelKey = "optipod.io/e2e-fail-probe"

// installedPodMutator reads the installed MutatingWebhookConfiguration and
// returns the pod mutator's webhook entry.
func installedPodMutator(ctx context.Context) admissionregv1.MutatingWebhook {
	mwc, err := env.Clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().
		Get(ctx, framework.WebhookConfigurationName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "read the installed MutatingWebhookConfiguration")
	var webhook admissionregv1.MutatingWebhook
	for _, w := range mwc.Webhooks {
		if w.Name == framework.PodMutatorWebhookName {
			webhook = w
		}
	}
	Expect(webhook.Name).To(Equal(framework.PodMutatorWebhookName),
		"the installed configuration lacks the %s mutating webhook", framework.PodMutatorWebhookName)
	return webhook
}

// webhookEndpointPod waits until the installed webhook Service has a ready
// endpoint and returns the Ready controller pod whose IP serves it, proving the
// endpoint is backed by a matching pod rather than merely present.
func webhookEndpointPod(ctx context.Context) corev1.Pod {
	Expect(framework.WaitForServiceEndpoints(ctx, env.Clients, framework.SystemNamespace,
		framework.WebhookServiceName, framework.WaitOptions{Timeout: 3 * time.Minute})).To(Succeed(),
		"the installed webhook Service never had a ready endpoint")
	ep, err := env.Clients.Kube.CoreV1().Endpoints(framework.SystemNamespace).
		Get(ctx, framework.WebhookServiceName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "read the installed webhook Service endpoints")
	addresses := map[string]bool{}
	for _, subset := range ep.Subsets {
		for _, address := range subset.Addresses {
			addresses[address.IP] = true
		}
	}
	Expect(addresses).NotTo(BeEmpty(), "the installed webhook Service has no ready endpoint addresses")

	pods, err := env.Clients.Kube.CoreV1().Pods(framework.SystemNamespace).List(ctx,
		metav1.ListOptions{LabelSelector: framework.ControllerPodSelector})
	Expect(err).NotTo(HaveOccurred(), "list the deployed controller pods")
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.PodIP == "" || !addresses[pod.Status.PodIP] {
			continue
		}
		Expect(isReady(pod)).To(BeTrue(),
			"the controller pod %s behind the webhook Service endpoint is not Ready", pod.Name)
		Expect(pod.Spec.ServiceAccountName).To(Equal(framework.ControllerServiceAccountName),
			"the controller pod serving the webhook runs as %q, want the shipped %q",
			pod.Spec.ServiceAccountName, framework.ControllerServiceAccountName)
		return *pod
	}
	Fail(fmt.Sprintf("no Ready controller pod IP matches the webhook Service endpoints %v", addresses))
	return corev1.Pod{}
}

// readInstalledServingCert reads the serving certificate the installed webhook
// endpoint presents (the harness-created TLS Secret the manager mounts) and
// REQUIRES it to chain to the installed caBundle with the webhook Service DNS
// name as the verified name. Only the public certificate is read; the private
// key is never accessed. The API server's own verification of this exact
// certificate is proven separately by the live admission mutation, since
// failurePolicy=Ignore would otherwise admit the pod unmutated.
func readInstalledServingCert(ctx context.Context, caPEM []byte) *x509.Certificate {
	secret, err := env.Clients.Kube.CoreV1().Secrets(framework.SystemNamespace).
		Get(ctx, framework.WebhookCertSecretName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "read the installed webhook serving-certificate Secret %s/%s",
		framework.SystemNamespace, framework.WebhookCertSecretName)
	certPEM, ok := secret.Data["tls.crt"]
	Expect(ok).To(BeTrue(), "the installed webhook serving-certificate Secret has no tls.crt entry")
	block, _ := pem.Decode(certPEM)
	Expect(block).NotTo(BeNil(), "the installed webhook serving certificate is not PEM-encoded")
	served, err := x509.ParseCertificate(block.Bytes)
	Expect(err).NotTo(HaveOccurred(), "parse the installed webhook serving certificate")

	roots := x509.NewCertPool()
	Expect(roots.AppendCertsFromPEM(caPEM)).To(BeTrue(),
		"the installed MutatingWebhookConfiguration caBundle is not a usable PEM CA")
	serviceDNS := fmt.Sprintf("%s.%s.svc", framework.WebhookServiceName, framework.SystemNamespace)
	_, err = served.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: x509.NewCertPool(),
		DNSName:       serviceDNS,
	})
	Expect(err).NotTo(HaveOccurred(),
		"the installed serving certificate does not chain to the installed caBundle for the webhook Service DNS name %q",
		serviceDNS)
	return served
}

// expectWebhookServerOutage scales the installed webhook server down and waits
// until the controller has no pods and the webhook Service has no endpoint
// addresses, so the outage is a confirmed state rather than an assumption.
func expectWebhookServerOutage(ctx context.Context) {
	scaleDeployment(ctx, framework.SystemNamespace, framework.ControllerDeploymentName, 0)
	err := framework.WaitForCondition(ctx, framework.WaitOptions{
		Timeout:     3 * time.Minute,
		Description: "the installed webhook server to be fully outaged (no controller pods and no Service endpoints)",
	}, func(ctx context.Context) (bool, error) {
		pods, err := env.Clients.Kube.CoreV1().Pods(framework.SystemNamespace).List(ctx,
			metav1.ListOptions{LabelSelector: framework.ControllerPodSelector})
		if err != nil {
			return false, err
		}
		if len(pods.Items) > 0 {
			return false, nil
		}
		ep, err := env.Clients.Kube.CoreV1().Endpoints(framework.SystemNamespace).
			Get(ctx, framework.WebhookServiceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, subset := range ep.Subsets {
			if len(subset.Addresses) > 0 {
				return false, nil
			}
		}
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(), "the installed webhook server never reported a complete outage")
}

// restoreWebhookServer scales the installed webhook server back up and waits
// until it is Available with a ready Service endpoint.
func restoreWebhookServer(ctx context.Context) {
	scaleDeployment(ctx, framework.SystemNamespace, framework.ControllerDeploymentName, 1)
	Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, framework.SystemNamespace,
		framework.ControllerDeploymentName, framework.WaitOptions{Timeout: 3 * time.Minute})).To(Succeed(),
		"the restored webhook server never became Available")
	Expect(framework.WaitForServiceEndpoints(ctx, env.Clients, framework.SystemNamespace,
		framework.WebhookServiceName, framework.WaitOptions{Timeout: 3 * time.Minute})).To(Succeed(),
		"the restored webhook Service never regained a ready endpoint")
}

// newFailProbePod builds a minimal pod carrying the scoped Fail
// configuration's object-selector label and NO installed-webhook enabling
// label, so only the scoped Fail configuration can match it.
func newFailProbePod(namespace, name, labelValue string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    map[string]string{"app": name, failProbeLabelKey: labelValue},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "app",
			Image: framework.WorkloadImage(),
		}}},
	}
}

func failurePolicyPtr(p admissionregv1.FailurePolicyType) *admissionregv1.FailurePolicyType {
	return &p
}

func sideEffectsPtr(s admissionregv1.SideEffectClass) *admissionregv1.SideEffectClass { return &s }

func int32Ptr(i int32) *int32 { return &i }

func stringPtr(s string) *string { return &s }
