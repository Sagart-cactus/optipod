package consolee2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/test/e2e/framework"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDeployedConsoleRestartsAndReadOnlyAccess(t *testing.T) {
	endpoint := os.Getenv("OPTIPOD_CONSOLE_E2E_URL")
	if testing.Short() || endpoint == "" {
		t.Skip("set OPTIPOD_CONSOLE_E2E_URL for the installed console restart test")
	}
	paths := filepath.SplitList(os.Getenv("OPTIPOD_CONSOLE_E2E_KUBECONFIGS"))
	if len(paths) != 2 {
		t.Fatal("two owned cluster kubeconfigs required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	clients := ownedClients(t, ctx, paths)
	token, err := os.ReadFile(os.Getenv("OPTIPOD_CONSOLE_E2E_VIEWER_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	login, _ := json.Marshal(map[string]string{"token": strings.TrimSpace(string(token))})
	response, err := browser.Post(endpoint+"/auth/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("login: %d", response.StatusCode)
	}
	var before []console.Cluster
	wait(t, ctx, "both deployed agents", func() bool {
		return getJSON(browser, endpoint+"/api/clusters", &before) == nil && len(before) == 2 && before[0].CapturedAt != nil && before[1].CapturedAt != nil
	})
	var fleet console.Page
	wait(t, ctx, "dummy workload inventory", func() bool {
		return getJSON(browser, endpoint+"/api/workloads?namespace=optipod-console-demo", &fleet) == nil && len(fleet.Items) == 6
	})
	for _, c := range clients {
		assertReadOnlyAgent(t, ctx, c)
	}
	// The same browser session must survive API replacement.
	deletePods(t, ctx, clients[0], map[string]string{"app.kubernetes.io/instance": "console", "app.kubernetes.io/component": "server"})
	wait(t, ctx, "API session after restart", func() bool { var me map[string]any; return getJSON(browser, endpoint+"/api/me", &me) == nil })
	// Agent PVCs preserve their stream identity and sequence across restarts.
	for _, c := range clients {
		deletePods(t, ctx, c, map[string]string{"app.kubernetes.io/instance": "console", "app.kubernetes.io/component": "agent"})
	}
	wait(t, ctx, "agent sequence continuity", func() bool {
		var after []console.Cluster
		if getJSON(browser, endpoint+"/api/clusters", &after) != nil || len(after) != 2 {
			return false
		}
		for i := range after {
			if after[i].UID != before[i].UID || after[i].LastSequence <= before[i].LastSequence {
				return false
			}
		}
		return true
	})
	// PostgreSQL restarts on the same PVC. Data and sessions must survive.
	deletePods(t, ctx, clients[0], map[string]string{"app": "console-postgres"})
	wait(t, ctx, "PostgreSQL data after restart", func() bool {
		var after console.Page
		return getJSON(browser, endpoint+"/api/workloads?namespace=optipod-console-demo", &after) == nil && len(after.Items) == len(fleet.Items)
	})
	r, err := browser.Post(endpoint+"/api/workloads", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal("shared API accepted a workload write")
	}
	t.Log("installed API, both agent PVCs and PostgreSQL restarted; browser session and inventory survived; source RBAC denies writes and secrets")
}

func getJSON(c *http.Client, url string, out any) error {
	r, err := c.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return json.NewDecoder(r.Body).Decode(out)
}

func wait(t *testing.T, ctx context.Context, description string, ready func() bool) {
	t.Helper()
	for {
		if ready() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out: %s", description)
		case <-time.After(2 * time.Second):
		}
	}
}

func deletePods(t *testing.T, ctx context.Context, c *framework.Clients, labels map[string]string) {
	t.Helper()
	pods := &corev1.PodList{}
	if err := c.Ctrl.List(ctx, pods, client.InNamespace("optipod-console-e2e"), client.MatchingLabels(labels)); err != nil {
		t.Fatal(err)
	}
	if len(pods.Items) == 0 {
		t.Fatal("expected installed test pods")
	}
	oldUIDs := map[string]bool{}
	for i := range pods.Items {
		oldUIDs[string(pods.Items[i].UID)] = true
		if err := c.Ctrl.Delete(ctx, &pods.Items[i]); err != nil {
			t.Fatal(err)
		}
	}
	wait(t, ctx, "replacement pods ready", func() bool {
		current := &corev1.PodList{}
		if c.Ctrl.List(ctx, current, client.InNamespace("optipod-console-e2e"), client.MatchingLabels(labels)) != nil || len(current.Items) == 0 {
			return false
		}
		for _, pod := range current.Items {
			if oldUIDs[string(pod.UID)] || pod.DeletionTimestamp != nil {
				return false
			}
			ready := false
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					ready = true
				}
			}
			if !ready {
				return false
			}
		}
		return true
	})
}

func assertReadOnlyAgent(t *testing.T, ctx context.Context, c *framework.Clients) {
	t.Helper()
	for _, probe := range []struct {
		group, resource, verb string
		allowed               bool
	}{{"apps", "deployments", "get", true}, {"apps", "deployments", "patch", false}, {"", "secrets", "get", false}} {
		review := &authv1.SubjectAccessReview{Spec: authv1.SubjectAccessReviewSpec{User: "system:serviceaccount:optipod-console-e2e:console-agent", ResourceAttributes: &authv1.ResourceAttributes{Namespace: "optipod-console-demo", Group: probe.group, Resource: probe.resource, Verb: probe.verb}}}
		result, err := c.Kube.AuthorizationV1().SubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
		if err != nil || result.Status.Allowed != probe.allowed {
			t.Fatalf("agent RBAC %+v: %v %v", probe, result, err)
		}
	}
}
