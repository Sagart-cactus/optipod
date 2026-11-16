package consoleinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func clusterProbe(ctx context.Context, t *Target, p *Plan, allowHTTP bool) (Report, error) {
	name := "optipod-probe-" + uuid.NewString()[:8]
	ns := p.Registration.Installation.Namespace
	if err := ensureNamespace(ctx, t, ns); err != nil {
		return Report{}, fmt.Errorf("cannot create or access the installation namespace")
	}
	defer cleanupProbe(t, ns, name)
	if err := createProbeAccess(ctx, t, ns, name); err != nil {
		return Report{}, fmt.Errorf("cannot create temporary probe RBAC; grant installer permissions")
	}
	_, err := t.Kube.CoreV1().ConfigMaps(ns).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name}, Data: map[string]string{"ca.crt": p.CAPEM}}, metav1.CreateOptions{})
	if err != nil {
		return Report{}, fmt.Errorf("cannot create temporary public CA ConfigMap")
	}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}}}}
	if p.Registration.Installation.StorageClass != "" {
		claim.Spec.StorageClassName = &p.Registration.Installation.StorageClass
	}
	if _, err = t.Kube.CoreV1().PersistentVolumeClaims(ns).Create(ctx, claim, metav1.CreateOptions{}); err != nil {
		return Report{}, fmt.Errorf("cannot create probe PVC")
	}
	job := probeJob(p, ns, name, allowHTTP)
	// Server-side dry-run reaches admission webhooks without persisting a Pod.
	_, err = t.Kube.CoreV1().Pods(ns).Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-admission", Labels: job.Spec.Template.Labels}, Spec: job.Spec.Template.Spec}, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return Report{}, fmt.Errorf("admission dry-run rejected the probe; check webhook availability and namespace policies")
	}
	if _, err = t.Kube.BatchV1().Jobs(ns).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return Report{}, fmt.Errorf("cannot create diagnostic Job")
	}
	deadline := time.NewTimer(200 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		current, err := t.Kube.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return Report{}, fmt.Errorf("cannot observe diagnostic Job")
		}
		if current.Status.Succeeded > 0 || current.Status.Failed > 0 {
			break
		}
		select {
		case <-ctx.Done():
			return Report{}, ctx.Err()
		case <-deadline.C:
			return Report{}, fmt.Errorf("diagnostic Job did not finish; verify image pulls, StorageClass provisioning and namespace egress")
		case <-ticker.C:
		}
	}
	pods, err := t.Kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + name})
	if err != nil || len(pods.Items) != 1 {
		return Report{}, fmt.Errorf("diagnostic Pod unavailable")
	}
	limit := int64(65536)
	data, err := t.Kube.CoreV1().Pods(ns).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{Container: probeName, LimitBytes: &limit}).DoRaw(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("cannot read diagnostic results; grant get pods/log")
	}
	var report Report
	// The process may add a sanitized failure line after its structured report.
	if json.NewDecoder(strings.NewReader(string(data))).Decode(&report) != nil || len(report.Checks) == 0 {
		return Report{}, fmt.Errorf("diagnostic image did not return a compatible report")
	}
	report.Add("webhook", "pass", "Server-side Pod admission dry-run passed")
	return report, nil
}

func createProbeAccess(ctx context.Context, t *Target, ns, name string) error {
	if _, err := t.Kube.CoreV1().ServiceAccounts(ns).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{}); err != nil {
		return err
	}
	role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"metrics.k8s.io"}, Resources: []string{podsResource}, Verbs: []string{getVerb, listVerb}}}}
	if _, err := t.Kube.RbacV1().ClusterRoles().Create(ctx, role, metav1.CreateOptions{}); err != nil {
		return err
	}
	binding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}, RoleRef: rbacv1.RoleRef{APIGroup: rbacGroup, Kind: "ClusterRole", Name: name}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: ns, Name: name}}}
	_, err := t.Kube.RbacV1().ClusterRoleBindings().Create(ctx, binding, metav1.CreateOptions{})
	return err
}

func cleanupProbe(t *Target, ns, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	policy := metav1.DeletePropagationBackground
	_ = t.Kube.BatchV1().Jobs(ns).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy})
	_ = t.Kube.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = t.Kube.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = t.Kube.CoreV1().ServiceAccounts(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = t.Kube.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{})
	_ = t.Kube.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{})
}

func probeJob(p *Plan, ns, name string, allowHTTP bool) *batchv1.Job {
	zero := int32(0)
	ttl := int32(120)
	deadline := int64(180)
	user := int64(65532)
	yes, no := true, false
	args := []string{probeName, "--server-url=" + p.ServerURL, "--ca-file=/etc/console-ca/ca.crt", "--metrics-source=" + p.Registration.MetricsSource, "--prometheus-url=" + p.Registration.Installation.PrometheusURL, "--namespaces=" + strings.Join(p.Registration.Namespaces, ","), "--volume-path=/probe-volume"}
	if allowHTTP {
		args = append(args, "--allow-insecure-http")
	}
	container := corev1.Container{Name: probeName, Image: p.Image, ImagePullPolicy: corev1.PullIfNotPresent, Args: args, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &yes, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")}}, VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/probe-volume"}, {Name: "ca", MountPath: "/etc/console-ca", ReadOnly: true}}}
	volumes := []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name}}}, {Name: "ca", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}}}}
	if p.PrometheusSecret != "" {
		volumes = append(volumes, corev1.Volume{Name: "metrics-auth", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: p.PrometheusSecret}}})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "metrics-auth", MountPath: "/etc/metrics-auth", ReadOnly: true})
		container.Args = append(container.Args, "--prometheus-auth="+p.PrometheusAuth, "--prometheus-auth-dir=/etc/metrics-auth")
	}
	if p.PrometheusCASecret != "" {
		volumes = append(volumes, corev1.Volume{Name: "metrics-ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: p.PrometheusCASecret}}})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "metrics-ca", MountPath: "/etc/metrics-ca", ReadOnly: true})
		container.Args = append(container.Args, "--prometheus-ca-file=/etc/metrics-ca/ca.crt")
	}
	if p.ProxySecret != "" {
		container.EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: p.ProxySecret}}}}
	}
	pulls := make([]corev1.LocalObjectReference, 0, len(p.ImagePullSecrets))
	for _, name := range p.ImagePullSecrets {
		pulls = append(pulls, corev1.LocalObjectReference{Name: name})
	}
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: batchv1.JobSpec{BackoffLimit: &zero, TTLSecondsAfterFinished: &ttl, ActiveDeadlineSeconds: &deadline, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/name": "optipod-onboarding-probe"}}, Spec: corev1.PodSpec{ServiceAccountName: name, ImagePullSecrets: pulls, RestartPolicy: corev1.RestartPolicyNever, SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &yes, RunAsUser: &user, RunAsGroup: &user, FSGroup: &user, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{container}, Volumes: volumes}}}}
}
