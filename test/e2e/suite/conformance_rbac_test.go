package suite

import (
	"context"
	"slices"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authzv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/framework"
)

// The shipped, name-prefixed RBAC objects config/default installs (config/rbac
// with the optipod- prefix and optipod-system namespace). The bindings assert
// below proves the controller ServiceAccount carries exactly these grants.
const (
	controllerClusterRoleBindingName         = "optipod-manager-rolebinding"
	controllerMetricsAuthRoleBindingName     = "optipod-metrics-auth-rolebinding"
	controllerLeaderElectionRoleBindingName  = "optipod-leader-election-rolebinding"
	controllerLeaderElectionRoleBindingScope = framework.SystemNamespace
)

// This scenario proves the shipped authorisation story on an installed cluster
// (VAL-CONF-011). It does three things with real API state:
//
//   - Reads the installed controller ServiceAccount, ClusterRole and bindings
//     and requires the ClusterRole to grant the verbs the deployed controller
//     needs (policy CRUD plus status, workload reads/updates, pods, events,
//     ReplicaSet parent lookup, webhook configuration). It also requires the
//     ServiceAccount's bindings to be exactly the shipped set, so no test
//     added a compensating grant that could mask a missing shipped permission.
//   - Proves the deployed controller actually reconciles, updates policy status
//     and applies the approved workload behavior under that ServiceAccount — a
//     real successful deployed-controller path, not an impersonation probe.
//   - Creates a restricted, unrelated ServiceAccount with no bindings and shows
//     a representative protected action is denied, both through a real
//     impersonated API call (Forbidden) and a corroborating SubjectAccessReview.
var _ = Describe("Shipped controller RBAC succeeds while restricted access is denied", Label("conformance", "rbac"), func() {
	It("runs the deployed controller on the shipped roles with no compensating grants and denies a restricted identity", func(ctx context.Context) {
		controllerSA := "system:serviceaccount:" + framework.SystemNamespace + ":" + framework.ControllerServiceAccountName
		controllerGroups := []string{
			"system:serviceaccounts",
			"system:serviceaccounts:" + framework.SystemNamespace,
			"system:authenticated",
		}

		By("asserting the installed controller ServiceAccount and ClusterRole exist")
		sa, err := env.Clients.Kube.CoreV1().ServiceAccounts(framework.SystemNamespace).
			Get(ctx, framework.ControllerServiceAccountName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "read the installed controller ServiceAccount %s/%s",
			framework.SystemNamespace, framework.ControllerServiceAccountName)
		Expect(sa.Name).To(Equal(framework.ControllerServiceAccountName),
			"the controller ServiceAccount has an unexpected identity")

		role, err := env.Clients.Kube.RbacV1().ClusterRoles().
			Get(ctx, framework.ControllerClusterRoleName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "read the installed controller ClusterRole %s",
			framework.ControllerClusterRoleName)

		By("requiring the installed ClusterRole to grant the deployed controller's required verbs")
		requiredGrants := []struct {
			group    string
			resource string
			verbs    []string
		}{
			{"optipod.optipod.io", "optimizationpolicies", []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
			{"optipod.optipod.io", "optimizationpolicies/status", []string{"get", "update", "patch"}},
			{"apps", "replicasets", []string{"get", "list", "watch"}},
			{"apps", "deployments", []string{"get", "list", "watch", "update", "patch"}},
			{"apps", "statefulsets", []string{"get", "list", "watch", "update", "patch"}},
			{"apps", "daemonsets", []string{"get", "list", "watch", "update", "patch"}},
			{"", "pods", []string{"get", "list", "watch"}},
			{"", "namespaces", []string{"get", "list", "watch"}},
			{"", "events", []string{"create", "patch"}},
			{"admissionregistration.k8s.io", "mutatingadmissionwebhookconfigurations", []string{"get", "list", "watch", "update", "patch"}},
		}
		for _, grant := range requiredGrants {
			expectClusterRoleAllows(role, grant.group, grant.resource, grant.verbs...)
		}

		By("asserting the controller ServiceAccount's bindings are exactly the shipped grants")
		crbs, err := env.Clients.Kube.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "list ClusterRoleBindings")
		crbNames := clusterRoleBindingNamesForSA(crbs.Items, sa.Name, sa.Namespace)
		Expect(crbNames).To(ConsistOf(controllerClusterRoleBindingName, controllerMetricsAuthRoleBindingName),
			"the controller ServiceAccount %s is bound to unexpected ClusterRoleBindings %v; the shipped install binds exactly %v (no compensating grants)",
			controllerSA, crbNames,
			[]string{controllerClusterRoleBindingName, controllerMetricsAuthRoleBindingName})
		managerBinding, err := env.Clients.Kube.RbacV1().ClusterRoleBindings().
			Get(ctx, controllerClusterRoleBindingName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "read the shipped manager ClusterRoleBinding")
		Expect(managerBinding.RoleRef.Kind).To(Equal("ClusterRole"),
			"the manager ClusterRoleBinding references %s, not a ClusterRole", managerBinding.RoleRef.Kind)
		Expect(managerBinding.RoleRef.Name).To(Equal(framework.ControllerClusterRoleName),
			"the manager ClusterRoleBinding references %s, want the shipped %s",
			managerBinding.RoleRef.Name, framework.ControllerClusterRoleName)

		rbs, err := env.Clients.Kube.RbacV1().RoleBindings(framework.SystemNamespace).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "list RoleBindings in %s", framework.SystemNamespace)
		rbNames := roleBindingNamesForSA(rbs.Items, sa.Name, sa.Namespace)
		Expect(rbNames).To(ConsistOf(controllerLeaderElectionRoleBindingName),
			"the controller ServiceAccount %s is bound to unexpected RoleBindings %v in %s; the shipped install binds exactly %v",
			controllerSA, rbNames, controllerLeaderElectionRoleBindingScope,
			[]string{controllerLeaderElectionRoleBindingName})

		By("asserting the shipped roles authorize the controller's representative actions")
		for _, attrs := range []authzv1.ResourceAttributes{
			{Verb: "update", Group: "optipod.optipod.io", Version: "v1alpha1", Resource: "optimizationpolicies", Subresource: "status"},
			{Verb: "patch", Group: "apps", Version: "v1", Resource: "deployments"},
			{Verb: "get", Group: "apps", Version: "v1", Resource: "replicasets"},
			{Verb: "create", Group: "", Version: "v1", Resource: "events"},
			{Verb: "list", Group: "", Version: "v1", Resource: "pods"},
		} {
			review := &authzv1.SubjectAccessReview{Spec: authzv1.SubjectAccessReviewSpec{
				User:               controllerSA,
				Groups:             controllerGroups,
				ResourceAttributes: &attrs,
			}}
			created, err := env.Clients.Kube.AuthorizationV1().SubjectAccessReviews().
				Create(ctx, review, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "create SubjectAccessReview for the controller ServiceAccount")
			Expect(created.Status.Allowed).To(BeTrue(),
				"the shipped roles do not authorize the controller ServiceAccount for %+v (reason: %s)",
				attrs, created.Status.Reason)
		}

		By("proving the deployed controller reconciles, updates status and applies under the shipped ServiceAccount")
		ns := specNamespace(ctx, "conf-rbac")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, ns.Name)).To(Succeed(),
				"clean up the RBAC conformance spec namespace")
		})
		samples := smokeSamples()
		workloadName, err := framework.UniqueName("workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique RBAC workload name")
		workload := framework.NewDeploymentFixture(ns.Name, workloadName, 1, smokeContainers(samples)...)
		Expect(env.Clients.Ctrl.Create(ctx, workload)).To(Succeed(), "create the RBAC workload")
		Expect(framework.WaitForDeploymentAvailable(ctx, env.Clients, ns.Name, workloadName,
			framework.WaitOptions{Timeout: conformanceWorkloadReadyTimeout})).To(Succeed(),
			"the RBAC workload never became Available")
		registerWorkloadSeries(ctx, ns.Name, workloadName, samples)
		Expect(env.Fixture.ClearTrace(ctx)).To(Succeed(), "reset the fixture trace for this spec")

		policyName, err := framework.UniqueName("policy")
		Expect(err).NotTo(HaveOccurred(), "generate unique policy name")
		policy := framework.NewPolicyFixture(ns.Name, policyName, optipodv1alpha1.ModeAuto,
			framework.WithStrategy("ssa", ""),
			framework.WithReconciliationInterval(30*time.Second))
		Expect(env.Clients.Ctrl.Create(ctx, policy)).To(Succeed(), "create the RBAC Auto policy")
		createdAt := policy.CreationTimestamp.Time
		// The apply proves the workload update path; the status wait proves the
		// optimizationpolicies/status update path. Both run under the shipped
		// ServiceAccount because no other binding exists.
		waitForWorkloadTemplateRequests(ctx, optipodv1alpha1.WorkloadTypeDeployment, ns.Name, workloadName, samples)
		waitForPolicyStatus(ctx, ns.Name, policyName, policyStatusWait{
			readyStatus:     "True",
			readyReason:     "PolicyValid",
			expectSummary:   true,
			discovered:      1,
			processed:       1,
			reconciledAfter: createdAt,
		})
		controllerPods, err := env.Clients.Kube.CoreV1().Pods(framework.SystemNamespace).List(ctx,
			metav1.ListOptions{LabelSelector: framework.ControllerPodSelector})
		Expect(err).NotTo(HaveOccurred(), "list the deployed controller pods")
		Expect(controllerPods.Items).NotTo(BeEmpty(), "no deployed controller pod found")
		for i := range controllerPods.Items {
			Expect(controllerPods.Items[i].Spec.ServiceAccountName).To(Equal(framework.ControllerServiceAccountName),
				"deployed controller pod %s runs as %q, not the shipped %q",
				controllerPods.Items[i].Name, controllerPods.Items[i].Spec.ServiceAccountName,
				framework.ControllerServiceAccountName)
		}

		By("creating a restricted unrelated identity with no bindings")
		restrictedNS := specNamespace(ctx, "conf-rbac-restricted")
		DeferCleanup(func(ctx context.Context) {
			Expect(framework.DeleteNamespace(ctx, env.Clients, restrictedNS.Name)).To(Succeed(),
				"clean up the restricted-identity spec namespace")
		})
		restrictedName, err := framework.UniqueName("restricted")
		Expect(err).NotTo(HaveOccurred(), "generate unique restricted ServiceAccount name")
		_, err = env.Clients.Kube.CoreV1().ServiceAccounts(restrictedNS.Name).Create(ctx,
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: restrictedNS.Name, Name: restrictedName}},
			metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "create the restricted ServiceAccount")
		restrictedUser := "system:serviceaccount:" + restrictedNS.Name + ":" + restrictedName
		restrictedGroups := []string{
			"system:serviceaccounts",
			"system:serviceaccounts:" + restrictedNS.Name,
			"system:authenticated",
		}

		By("denying the restricted identity a representative protected action through real API calls")
		restrictedCfg := rest.CopyConfig(env.Clients.RESTConfig())
		restrictedCfg.Impersonate = rest.ImpersonationConfig{UserName: restrictedUser, Groups: restrictedGroups}
		restricted, err := framework.NewClients(restrictedCfg)
		Expect(err).NotTo(HaveOccurred(), "build restricted-identity clients")

		// Action 1: cluster-wide list of the product CR.
		err = restricted.Ctrl.List(ctx, &optipodv1alpha1.OptimizationPolicyList{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(),
			"the restricted identity was not denied a cluster-wide OptimizationPolicy list (got %v)", err)

		// Action 2: create a Deployment in the restricted namespace it owns.
		deniedName, err := framework.UniqueName("denied-workload")
		Expect(err).NotTo(HaveOccurred(), "generate unique denied-workload name")
		denied := framework.NewDeploymentFixture(restrictedNS.Name, deniedName, 1,
			framework.ContainerFixture{Name: "app", CPURequest: "10m", MemoryRequest: "16Mi"})
		_, err = restricted.Kube.AppsV1().Deployments(restrictedNS.Name).Create(ctx, denied, metav1.CreateOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(),
			"the restricted identity was not denied creating a Deployment in its own namespace (got %v)", err)

		// Corroborating SubjectAccessReview: the restricted identity has no
		// authorizer that allows the action. (Status.Denied is only set by an
		// explicit deny rule, not by the absence of a granting binding, so the
		// assertion is on Allowed remaining false.)
		deniedAttrs := authzv1.ResourceAttributes{
			Namespace: restrictedNS.Name, Verb: "create", Group: "apps", Version: "v1", Resource: "deployments",
		}
		review := &authzv1.SubjectAccessReview{Spec: authzv1.SubjectAccessReviewSpec{
			User:               restrictedUser,
			Groups:             restrictedGroups,
			ResourceAttributes: &deniedAttrs,
		}}
		created, err := env.Clients.Kube.AuthorizationV1().SubjectAccessReviews().
			Create(ctx, review, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "create the corroborating SubjectAccessReview")
		Expect(created.Status.Allowed).To(BeFalse(),
			"the restricted identity was unexpectedly authorized for %+v", deniedAttrs)
	})
})

// expectClusterRoleAllows requires the ClusterRole to grant every verb on the
// group/resource pair, unioning the verbs of every matching rule. A resource
// may carry a subresource (e.g. "optimizationpolicies/status").
func expectClusterRoleAllows(role *rbacv1.ClusterRole, group, resource string, verbs ...string) {
	var granted []string
	for _, rule := range role.Rules {
		if !slices.Contains(rule.APIGroups, group) && !slices.Contains(rule.APIGroups, "*") {
			continue
		}
		if !slices.Contains(rule.Resources, resource) && !slices.Contains(rule.Resources, "*") {
			continue
		}
		granted = append(granted, rule.Verbs...)
	}
	for _, verb := range verbs {
		Expect(granted).To(ContainElement(verb),
			"the installed ClusterRole %s grants %s %q %v, want the shipped grant to include %q",
			role.Name, groupLabel(group), resource, granted, verb)
	}
}

// groupLabel renders the core API group legibly in assertions.
func groupLabel(group string) string {
	if group == "" {
		return "core/"
	}
	return group + "/"
}

// clusterRoleBindingNamesForSA returns the sorted names of the ClusterRoleBindings
// whose subjects include the named ServiceAccount.
func clusterRoleBindingNamesForSA(bindings []rbacv1.ClusterRoleBinding, name, namespace string) []string {
	names := make([]string, 0, len(bindings))
	for i := range bindings {
		if bindingReferencesSA(bindings[i].Subjects, name, namespace) {
			names = append(names, bindings[i].Name)
		}
	}
	sort.Strings(names)
	return names
}

// roleBindingNamesForSA returns the sorted names of the RoleBindings whose
// subjects include the named ServiceAccount.
func roleBindingNamesForSA(bindings []rbacv1.RoleBinding, name, namespace string) []string {
	names := make([]string, 0, len(bindings))
	for i := range bindings {
		if bindingReferencesSA(bindings[i].Subjects, name, namespace) {
			names = append(names, bindings[i].Name)
		}
	}
	sort.Strings(names)
	return names
}

func bindingReferencesSA(subjects []rbacv1.Subject, name, namespace string) bool {
	for _, subject := range subjects {
		if subject.Kind == "ServiceAccount" && subject.Name == name && subject.Namespace == namespace {
			return true
		}
	}
	return false
}
