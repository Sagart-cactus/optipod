package consolee2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const consoleNamespace = "optipod-console-e2e"

func TestManagedAgentSecretRotation(t *testing.T) {
	ctx, clients, paths := helmSecretEnvironment(t)
	peer := clients[1]
	store := openStore(t, ctx)
	originalValues := helmValues(t, ctx, paths[1])
	originalFile := privateValuesFile(t, originalValues)
	originalSecret := agentSecret(t, ctx, peer, "console-agent-auth")
	if originalSecret.Annotations["meta.helm.sh/release-name"] != "console" {
		t.Fatal("peer agent credential is not owned by its Helm release")
	}
	originalPod, err := peer.Kube.CoreV1().Pods(consoleNamespace).Get(ctx, "console-agent-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	originalPVC, err := peer.Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Get(ctx, "queue-console-agent-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Reusing values must preserve both the credential and the running pod.
	helmCommand(t, ctx, paths[1], "upgrade", "console", consoleChartPath(), "--reuse-values", "--wait", "--timeout", "180s")
	after := agentSecret(t, ctx, peer, "console-agent-auth")
	pod, err := peer.Kube.CoreV1().Pods(consoleNamespace).Get(ctx, originalPod.Name, metav1.GetOptions{})
	if err != nil || pod.UID != originalPod.UID || after.UID != originalSecret.UID || string(after.Data["token"]) != string(originalSecret.Data["token"]) {
		t.Fatal("unchanged Helm values unexpectedly rotated the credential or restarted the agent")
	}
	// Cleanup restores the original server and agent credentials even on failure.
	t.Cleanup(func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		helmCommand(t, restoreCtx, paths[1], "upgrade", "console", consoleChartPath(), "--reset-values", "-f", originalFile, "--wait", "--timeout", "150s")
	})
	serverSecret := agentSecret(t, ctx, clients[0], "console-server")
	originalConfig := append([]byte(nil), serverSecret.Data["config.json"]...)
	t.Cleanup(func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		replaceServerConfig(t, restoreCtx, clients[0], originalConfig)
	})
	var cfg console.Config
	if err = json.Unmarshal(originalConfig, &cfg); err != nil {
		t.Fatal(err)
	}
	newToken := uuid.NewString() + uuid.NewString()
	found := false
	for i := range cfg.Agents {
		if cfg.Agents[i].ID == "local-prod" {
			cfg.Agents[i].TokenHash = console.TokenHash(newToken)
			found = true
		}
	}
	if !found {
		t.Fatal("local-prod registration missing")
	}
	configData, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	replaceServerConfig(t, ctx, clients[0], configData)
	var stream string
	var sequence int64
	if err = store.Pool.QueryRow(ctx, "SELECT stream_id, last_seq FROM op_clusters WHERE id='local-prod'").Scan(&stream, &sequence); err != nil {
		t.Fatal(err)
	}
	patch := privateValuesFile(t, map[string]any{"agent": map[string]any{"secret": map[string]any{"token": newToken}}})
	helmCommand(t, ctx, paths[1], "upgrade", "console", consoleChartPath(), "--reuse-values", "-f", patch, "--wait", "--timeout", "180s")
	after = agentSecret(t, ctx, peer, "console-agent-auth")
	pod, err = peer.Kube.CoreV1().Pods(consoleNamespace).Get(ctx, originalPod.Name, metav1.GetOptions{})
	if err != nil || pod.UID == originalPod.UID || pod.Annotations["checksum/agent-secret"] != console.TokenHash(newToken) || string(after.Data["token"]) != newToken {
		t.Fatal("token upgrade did not update the Secret and replace the agent pod")
	}
	pvc, err := peer.Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Get(ctx, originalPVC.Name, metav1.GetOptions{})
	if err != nil || pvc.UID != originalPVC.UID {
		t.Fatal("credential rotation replaced the persistent queue")
	}
	wait(t, ctx, "authenticated ingestion after token rotation", func() bool {
		var nextStream string
		var nextSequence int64
		err := store.Pool.QueryRow(ctx, "SELECT stream_id, last_seq FROM op_clusters WHERE id='local-prod'").Scan(&nextStream, &nextSequence)
		return err == nil && nextStream == stream && nextSequence > sequence
	})
	t.Log("unchanged upgrade preserved pod and Secret; rotated credential authenticated with the same queue and stream")
}

func TestAgentSecretUninstall(t *testing.T) {
	ctx, clients, paths := helmSecretEnvironment(t)
	peer := clients[1]
	installedValues := helmValues(t, ctx, paths[1])
	for _, managed := range []bool{true, false} {
		name := "managed"
		if !managed {
			name = "external"
		}
		t.Run(name, func(t *testing.T) {
			release := "secret-e2e-" + uuid.NewString()[:8]
			secretName := release + "-agent-auth" // pragma: allowlist secret - Kubernetes Secret name
			token := uuid.NewString()
			values := agentChartValues("", managed, token)
			values["image"] = installedValues["image"]
			agent := values["agent"].(map[string]any)
			agent["clusterID"] = release
			agent["serverURL"] = installedValues["agent"].(map[string]any)["serverURL"]
			agent["allowInsecureHTTP"] = true
			if !managed {
				secretName = release + "-external" // pragma: allowlist secret - Kubernetes Secret name
				agent["existingSecret"] = secretName // pragma: allowlist secret - Kubernetes Secret name
				agent["secret"] = map[string]any{"create": false, "token": ""}
				_, err := peer.Kube.CoreV1().Secrets(consoleNamespace).Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName}, Data: map[string][]byte{"token": []byte(token)},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				// Only disposable resources created by this subtest are removed.
				defer func() {
					_ = peer.Kube.CoreV1().PersistentVolumeClaims(consoleNamespace).Delete(cleanupCtx, "queue-"+release+"-agent-0", metav1.DeleteOptions{})
					_ = peer.Kube.CoreV1().Secrets(consoleNamespace).Delete(cleanupCtx, secretName, metav1.DeleteOptions{})
				}()
				helmCommand(t, cleanupCtx, paths[1], "uninstall", release, "--ignore-not-found", "--wait", "--timeout", "90s")
			})
			helmCommand(t, ctx, paths[1], "install", release, consoleChartPath(), "-f", privateValuesFile(t, values), "--wait", "--timeout", "180s")
			secret := agentSecret(t, ctx, peer, secretName)
			if string(secret.Data["token"]) != token || managed != (secret.Annotations["meta.helm.sh/release-name"] == release) {
				t.Fatal("installed credential or Helm ownership incorrect")
			}
			helmCommand(t, ctx, paths[1], "uninstall", release, "--wait", "--timeout", "90s")
			remaining, err := peer.Kube.CoreV1().Secrets(consoleNamespace).Get(ctx, secretName, metav1.GetOptions{})
			if managed {
				if !apierrors.IsNotFound(err) {
					t.Fatal("Helm-managed Secret remained after uninstall")
				}
			} else if err != nil || remaining.UID != secret.UID || string(remaining.Data["token"]) != token {
				t.Fatal("uninstall removed or modified the external Secret")
			}
		})
	}
}

func helmSecretEnvironment(t *testing.T) (context.Context, []*framework.Clients, []string) {
	t.Helper()
	paths := filepath.SplitList(os.Getenv("OPTIPOD_CONSOLE_E2E_KUBECONFIGS"))
	if testing.Short() || os.Getenv("OPTIPOD_CONSOLE_E2E_URL") == "" || len(paths) != 2 {
		t.Skip("set installed console URL and two owned cluster kubeconfigs for Helm lifecycle E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	t.Cleanup(cancel)
	return ctx, ownedClients(t, ctx, paths), paths
}

func helmCommand(t *testing.T, ctx context.Context, kubeconfig string, args ...string) []byte {
	t.Helper()
	args = append(args, "--kubeconfig", kubeconfig, "--namespace", consoleNamespace)
	output, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	if err != nil {
		// Helm may print rendered credentials. Do not include its output in CI logs.
		t.Fatalf("helm %s failed: %v (output omitted to protect credentials)", args[0], err)
	}
	return output
}

func helmValues(t *testing.T, ctx context.Context, kubeconfig string) map[string]any {
	t.Helper()
	var values map[string]any
	if err := json.Unmarshal(helmCommand(t, ctx, kubeconfig, "get", "values", "console", "--output", "json"), &values); err != nil {
		t.Fatal("cannot decode installed Helm values")
	}
	return values
}

func agentSecret(t *testing.T, ctx context.Context, c *framework.Clients, name string) *corev1.Secret {
	t.Helper()
	secret, err := c.Kube.CoreV1().Secrets(consoleNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func replaceServerConfig(t *testing.T, ctx context.Context, c *framework.Clients, data []byte) {
	t.Helper()
	secret := agentSecret(t, ctx, c, "console-server")
	secret.Data["config.json"] = data
	if _, err := c.Kube.CoreV1().Secrets(consoleNamespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	deletePods(t, ctx, c, map[string]string{"app.kubernetes.io/instance": "console", "app.kubernetes.io/component": "server"})
}
