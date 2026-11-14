// Local development driver. All writes target a private, harness-owned cluster.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"

	"github.com/optipod/optipod/test/e2e/framework"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type state struct {
	Name       string
	Kubeconfig string
}

func main() {
	action := flag.String("action", "up", "up, reload, test, or down")
	labels := flag.String("labels", "smoke", "Ginkgo labels for test")
	flag.Parse()
	if err := run(*action, *labels); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(action, labels string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	root := framework.DefaultRepoRoot()
	path := filepath.Join(root, ".dev", "cluster.json")
	var s state
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &s); err != nil {
			return err
		}
	}
	if action == "up" {
		if data != nil {
			return fmt.Errorf("cluster state already exists; use reload or down: %s", path)
		}
		env, err := framework.SetupEnvironment(ctx, framework.EnvironmentOptions{
			Cluster: framework.ClusterOptions{
				Name: "optipod-e2e-dev", Keep: true, ConfigPath: filepath.Join(root, "hack/dev/kind-multi.yaml"),
			}, RepoRoot: root,
		})
		if err != nil {
			return err
		}
		s = state{env.Cluster.Name, env.Cluster.Kubeconfig}
		data, _ = json.MarshalIndent(s, "", "  ")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
		fmt.Printf("Cluster %s ready; private kubeconfig: %s\n", s.Name, s.Kubeconfig)
		return nil
	}
	if data == nil {
		return fmt.Errorf("no local cluster state; run dev up first")
	}
	cluster, err := framework.ReuseOwnedCluster(ctx, s.Name, s.Kubeconfig, framework.ClusterOptions{})
	if err != nil {
		return err
	}
	switch action {
	case "jvm":
		if err := command(ctx, "podman", "build", "-t", "docker.io/optipod-e2e/jvm:dev",
			filepath.Join(root, "test/e2e/jvm")); err != nil {
			return err
		}
		_, err := framework.NewImageBuilder(root).LoadImage(ctx, s.Name, "docker.io/optipod-e2e/jvm:dev")
		return err
	case "addons":
		return installAddons(ctx, root, s)
	case "backends":
		return backends(ctx, root, s)
	case "reload":
		return reload(ctx, root, s, cluster)
	case "test":
		if err := os.Setenv(framework.ReuseClusterEnvVar, s.Name); err != nil {
			return err
		}
		if err := os.Setenv(framework.ReuseKubeconfigEnvVar, s.Kubeconfig); err != nil {
			return err
		}
		return command(ctx, "go", "test", "-v", "-count=1", "-timeout=60m",
			"./test/e2e/suite", "-ginkgo.v", "-ginkgo.label-filter="+labels)
	case "down":
		if err := command(ctx, "kind", "delete", "cluster", "--name", s.Name, "--kubeconfig", s.Kubeconfig); err != nil {
			return err
		}
		return os.Remove(path)
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

func buildLoadImage(ctx context.Context, root, cluster string) error {
	dir := filepath.Join(root, ".dev", "load-image")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", filepath.Join(dir, "load"), "./test/e2e/load")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return err
	}
	dockerfile := "FROM gcr.io/distroless/static:nonroot\nCOPY load /load\nENTRYPOINT [\"/load\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0600); err != nil {
		return err
	}
	ref := "docker.io/optipod-e2e/load:dev"
	if err := command(ctx,
		framework.ContainerRuntime(),
		"build",
		"--platform",
		"linux/"+runtime.GOARCH,
		"-t",
		ref,
		dir); err != nil {
		return err
	}
	_, err := framework.NewImageBuilder(root).LoadImage(ctx, cluster, ref)
	return err
}

func command(ctx context.Context, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = framework.DefaultRepoRoot()
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func reload(ctx context.Context, root string, s state, cluster *framework.Cluster) error {
	b := framework.NewImageBuilder(root)
	img, err := b.BuildControllerImage(ctx)
	if err != nil {
		return err
	}
	if _, err = b.LoadImage(ctx, s.Name, img.Ref); err != nil {
		return err
	}
	crds, err := filepath.Glob(filepath.Join(root, "config/crd/bases/optipod.optipod.io_*.yaml"))
	if err != nil {
		return err
	}
	for _, crd := range crds {
		if err = s.kubectl(ctx, "apply", "--server-side", "-f", crd); err != nil {
			return err
		}
	}
	cfg, err := cluster.RESTConfig()
	if err != nil {
		return err
	}
	clients, err := framework.NewClients(cfg)
	if err != nil {
		return err
	}
	roleData, err := os.ReadFile(filepath.Join(root, "config/rbac/role.yaml"))
	if err != nil {
		return err
	}
	var desiredRole rbacv1.ClusterRole
	if err = yaml.Unmarshal(roleData, &desiredRole); err != nil {
		return err
	}
	installedRole,
		err := clients.Kube.RbacV1().ClusterRoles().Get(ctx,
		framework.ControllerClusterRoleName,
		metav1.GetOptions{})
	if err != nil {
		return err
	}
	installedRole.Rules = desiredRole.Rules
	if _, err = clients.Kube.RbacV1().ClusterRoles().Update(ctx, installedRole, metav1.UpdateOptions{}); err != nil {
		return err
	}
	wh,
		err := clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx,
		framework.WebhookConfigurationName,
		metav1.GetOptions{})
	if err != nil {
		return err
	}
	reinvoke := admissionregistrationv1.IfNeededReinvocationPolicy
	for i := range wh.Webhooks {
		wh.Webhooks[i].ReinvocationPolicy = &reinvoke
	}
	if _,
		err = clients.Kube.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(ctx,
		wh,
		metav1.UpdateOptions{}); err != nil {
		return err
	}
	fd,
		err := clients.Kube.AppsV1().Deployments(framework.FixtureNamespace).Get(ctx,
		framework.FixtureServiceName,
		metav1.GetOptions{})
	if err != nil {
		return err
	}
	fixture, err := fixtureImage(ctx, b, root, fd.Spec.Template.Spec.Containers[0].Image)
	if err != nil {
		return err
	}
	if fixture != fd.Spec.Template.Spec.Containers[0].Image {
		if _, err = b.LoadImage(ctx, s.Name, fixture); err != nil {
			return err
		}
		fd.Spec.Template.Spec.Containers[0].Image = fixture
		if _, err = clients.Kube.AppsV1().Deployments(fd.Namespace).Update(ctx, fd, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	if err = s.kubectl(ctx, "-n", fd.Namespace, "rollout", "status", "deployment/"+fd.Name, "--timeout=180s"); err != nil {
		return err
	}
	d,
		err := clients.Kube.AppsV1().Deployments(framework.SystemNamespace).Get(ctx,
		framework.ControllerDeploymentName,
		metav1.GetOptions{})
	if err != nil {
		return err
	}
	for i := range d.Spec.Template.Spec.Containers {
		if d.Spec.Template.Spec.Containers[i].Name == framework.ControllerContainerName {
			d.Spec.Template.Spec.Containers[i].Image = img.Ref
			d.Spec.Template.Spec.Containers[i].Command = []string{"/manager"}
		}
	}
	if _, err = clients.Kube.AppsV1().Deployments(d.Namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return s.kubectl(ctx, "-n", framework.SystemNamespace, "rollout", "status", "deployment/"+d.Name, "--timeout=180s")
}

func backends(ctx context.Context, root string, s state) error {
	if err := buildLoadImage(ctx, root, s.Name); err != nil {
		return err
	}
	for _, file := range []string{"prometheus.yaml", "metrics-server.yaml"} {
		if err := s.kubectl(ctx, "apply", "-f", filepath.Join(root, "test/e2e/backends", file)); err != nil {
			return err
		}
	}
	// Kind's kubelet serving certificate is self-signed. This override is
	// confined to the harness-owned cluster, never an installation default.
	patch := `{"spec":{"template":{"spec":{"containers":[{"name":"metrics-server","args":[
 "--cert-dir=/tmp","--secure-port=10250",
 "--kubelet-preferred-address-types=InternalIP,ExternalIP,Hostname",
 "--kubelet-use-node-status-port","--metric-resolution=15s","--kubelet-insecure-tls"]}]}}}}`
	if err := s.kubectl(ctx,
		"-n",
		"kube-system",
		"patch",
		"deployment/metrics-server",
		"--type=strategic",
		"-p",
		patch); err != nil {
		return err
	}
	for _, target := range [][2]string{{"optipod-e2e-backends", "prometheus"}, {"kube-system", "metrics-server"}} {
		if err := s.kubectl(ctx,
			"-n",
			target[0],
			"rollout",
			"status",
			"deployment/"+target[1],
			"--timeout=300s"); err != nil {
			return err
		}
	}
	return nil
}

func (s state) kubectl(ctx context.Context, args ...string) error {
	return command(ctx, "kubectl", append([]string{"--kubeconfig", s.Kubeconfig}, args...)...)
}
