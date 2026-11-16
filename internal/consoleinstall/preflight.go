package consoleinstall

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Target struct {
	Config *rest.Config
	Kube   *kubernetes.Clientset
}

func OpenTarget(path, contextName string) (*Target, error) {
	if path == "" || contextName == "" {
		return nil, fmt.Errorf("explicit kubeconfig and context are required")
	}
	raw, err := clientcmd.LoadFromFile(path)
	if err != nil || raw.Contexts[contextName] == nil {
		return nil, fmt.Errorf("cannot load the explicitly selected kubeconfig/context")
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, contextName, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("invalid Kubernetes context")
	}
	cfg.Timeout = 20 * time.Second
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Target{Config: cfg, Kube: kube}, nil
}

func VerifyBinding(ctx context.Context, t *Target, p *Plan) error {
	ns, err := t.Kube.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot read kube-system; check cluster connectivity and get namespace permission")
	}
	if string(ns.UID) != p.Registration.KubeUID {
		return fmt.Errorf("selected context does not match the registered cluster; no installation performed")
	}
	return nil
}

func Preflight(ctx context.Context, t *Target, p *Plan, allowHTTP bool) (Report, error) {
	return preflight(ctx, t, p, allowHTTP, false)
}

func preflight(ctx context.Context, t *Target, p *Plan, allowHTTP, resume bool) (Report, error) {
	r := Report{Checks: []Check{}}
	if err := p.Validate(allowHTTP); err != nil {
		return r, err
	}
	if err := VerifyBinding(ctx, t, p); err != nil {
		return r, err
	}
	info, err := t.Kube.Discovery().ServerVersion()
	if err != nil {
		return r, fmt.Errorf("kubernetes version unavailable")
	}
	v, err := version.ParseGeneric(info.GitVersion)
	if err != nil || v.Major() != 1 || v.Minor() < 33 || v.Minor() > 35 {
		r.Add("kubernetes-version", "fail", "This installer supports Kubernetes 1.33 through 1.35; use a compatible release")
	} else {
		r.Add("kubernetes-version", "pass", info.GitVersion)
	}
	permissionChecks(ctx, t, p, &r)
	controllerChecks(ctx, t, p, &r, resume)
	storageChecks(ctx, t, p, &r)
	if p.EnableWebhook {
		if _, err = t.Kube.Discovery().ServerResourcesForGroupVersion("cert-manager.io/v1"); err != nil {
			r.Add("webhook", "fail", "Install cert-manager before enabling the new controller webhook")
		} else {
			r.Add("webhook", "pass", "cert-manager API available")
		}
	}
	if !r.Ready() {
		return r, nil
	}
	probe, err := clusterProbe(ctx, t, p, allowHTTP)
	if err != nil {
		r.Add("cluster-probe", "fail", err.Error())
		return r, nil
	}
	r.Checks = append(r.Checks, probe.Checks...)
	return r, nil
}

func permissionChecks(ctx context.Context, t *Target, p *Plan, r *Report) {
	ns := p.Registration.Installation.Namespace
	resources := []struct{ group, resource, namespace string }{{"", "secrets", ns}, {"", "serviceaccounts", ns}, {"", "services", ns}, {"", "persistentvolumeclaims", ns}, {"", "configmaps", ns}, {appsGroup, "statefulsets", ns}, {"batch", "jobs", ns}, {rbacGroup, "clusterroles", ""}, {rbacGroup, "clusterrolebindings", ""}}
	if p.Registration.InstallMode == newControllerMode {
		resources = append(resources, struct{ group, resource, namespace string }{appsGroup, "deployments", p.Registration.Installation.ControllerNamespace}, struct{ group, resource, namespace string }{"apiextensions.k8s.io", "customresourcedefinitions", ""})
	}
	if p.Registration.InstallMode == newControllerMode {
		controllerNS := p.Registration.Installation.ControllerNamespace
		resources = append(resources, []struct{ group, resource, namespace string }{{"", "secrets", controllerNS}, {"", "configmaps", controllerNS}, {"", "serviceaccounts", controllerNS}, {"", "services", controllerNS}, {rbacGroup, "roles", controllerNS}, {rbacGroup, "rolebindings", controllerNS}, {"coordination.k8s.io", "leases", controllerNS}}...)
		if p.EnableWebhook {
			resources = append(resources, struct{ group, resource, namespace string }{"admissionregistration.k8s.io", "mutatingwebhookconfigurations", ""}, struct{ group, resource, namespace string }{"cert-manager.io", "issuers", controllerNS}, struct{ group, resource, namespace string }{"cert-manager.io", "certificates", controllerNS})
		}
	}
	for _, resource := range resources {
		for _, verb := range []string{getVerb, listVerb, "watch", createKey, "update", "patch", "delete"} {
			result, err := t.Kube.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authv1.ResourceAttributes{Group: resource.group, Resource: resource.resource, Namespace: resource.namespace, Verb: verb}}}, metav1.CreateOptions{})
			if err != nil || !result.Status.Allowed {
				r.Add("permissions", "fail", fmt.Sprintf("Grant %s on %s in %s before installing", verb, resource.resource, resource.namespace))
				return
			}
		}
	}
	for _, a := range []authv1.ResourceAttributes{{Resource: podsResource, Namespace: ns, Verb: createKey}, {Resource: podsResource, Namespace: ns, Verb: listVerb}, {Resource: podsResource, Subresource: "log", Namespace: ns, Verb: getVerb}, {Resource: namespacesKey, Verb: createKey}, {Resource: namespacesKey, Verb: getVerb}, {Group: appsGroup, Resource: "deployments", Verb: listVerb}, {Group: "storage.k8s.io", Resource: "storageclasses", Verb: listVerb}} {
		result, err := t.Kube.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &a}}, metav1.CreateOptions{})
		if err != nil || !result.Status.Allowed {
			r.Add("permissions", "fail", fmt.Sprintf("Grant %s on %s/%s for installer diagnostics", a.Verb, a.Resource, a.Subresource))
			return
		}
	}
	r.Add("permissions", "pass", "Required installer resource permissions granted")
}

func controllerChecks(ctx context.Context, t *Target, p *Plan, r *Report, resume bool) {
	deployments, err := t.Kube.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		r.Add("controller", "fail", "Cannot list deployments to detect existing OptiPod controllers")
		return
	}
	controllers := []appsv1.Deployment{}
	for _, deployment := range deployments.Items {
		if isController(deployment) {
			controllers = append(controllers, deployment)
		}
	}
	if len(controllers) > 1 {
		r.Add("controller", "fail", "Multiple optimizer controllers detected; resolve ownership before onboarding")
		return
	}
	if p.Registration.InstallMode == "existing" {
		if len(controllers) != 1 {
			r.Add("controller", "fail", "Existing-controller mode requires one installed optimizer")
		} else {
			r.Add("controller", "pass", "Reusing existing optimizer; only the telemetry agent will be installed")
		}
		return
	}
	if len(controllers) == 1 {
		d := controllers[0]
		i := p.Registration.Installation
		if !resume || d.Annotations["meta.helm.sh/release-name"] != i.ControllerRelease || d.Namespace != i.ControllerNamespace {
			r.Add("controller", "fail", "An optimizer already exists; choose existing-controller mode, or resume onboard with its saved state directory. Resources will not be adopted")
			return
		}
	}
	r.Add("controller", "pass", "New controller install or resume has no competing optimizer")
}

func isController(d appsv1.Deployment) bool {
	for _, container := range d.Spec.Template.Spec.Containers {
		if container.Name == "manager" || container.Name == "controller-manager" {
			if d.Labels["app.kubernetes.io/part-of"] == optimizerName || d.Labels["app.kubernetes.io/name"] == optimizerName || strings.HasPrefix(d.Labels["helm.sh/chart"], "optipod-") || strings.Contains(container.Image, optimizerName) || strings.Contains(d.Name, optimizerName) {
				return true
			}
		}
	}
	return false
}

func storageChecks(ctx context.Context, t *Target, p *Plan, r *Report) {
	classes, err := t.Kube.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		r.Add("storage-class", "fail", "Cannot list storage classes")
		return
	}
	name := p.Registration.Installation.StorageClass
	for _, class := range classes.Items {
		if class.Name == name || (name == "" && (class.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" || class.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true")) {
			r.Add("storage-class", "pass", "Using StorageClass "+class.Name)
			return
		}
	}
	r.Add("storage-class", "fail", "Select an available StorageClass for the durable agent queue")
}

func ensureNamespace(ctx context.Context, t *Target, namespace string) error {
	_, err := t.Kube.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = t.Kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{})
	return err
}
