package consolee2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestAgentSecretChart(t *testing.T) {
	token := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, existing, token, wantError string
		create, disabled                 bool
	}{
		{name: "external", existing: "external-agent"},
		{name: "managed", create: true, token: token},
		{name: "file newline", create: true, token: token + "\n"},
		{name: "disabled", create: true, disabled: true, token: token},
		{name: "no credential", wantError: "agent.existingSecret is required"},
		{name: "conflicting sources", create: true, existing: "external-agent", token: token, wantError: "choose either"},
		{name: "missing token", create: true, wantError: "at least 32 characters"},
		{name: "short token", create: true, token: "short", wantError: "at least 32 characters"},
		{name: "internal whitespace", create: true, token: token + "\n" + token, wantError: "no internal whitespace"},
		{name: "unused token", existing: "external-agent", token: token, wantError: "requires agent.secret.create=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := agentChartValues(tc.existing, tc.create, tc.token)
			values["agent"].(map[string]any)["enabled"] = !tc.disabled
			out, err := renderConsoleChart(t, values)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(string(out), tc.wantError) {
					t.Fatalf("expected chart validation error containing %q", tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("render chart: %v", err)
			}
			secret, agent := decodeAgentChart(t, out)
			if tc.disabled {
				if secret != nil || agent != nil { // pragma: allowlist secret - Kubernetes object comparison
					t.Fatal("disabled agent rendered credentials or a StatefulSet")
				}
				return
			}
			if agent == nil {
				t.Fatal("agent StatefulSet missing")
			}
			name := tc.existing
			if tc.create {
				name = "secret-test-agent-auth"
				if secret == nil || secret.Name != name || string(secret.Data["token"]) != token { // pragma: allowlist secret - generated fixture comparison
					t.Fatal("managed Secret name or normalized token is incorrect")
				}
				if secret.Labels["app.kubernetes.io/managed-by"] != "Helm" || secret.Annotations["helm.sh/resource-policy"] != "" {
					t.Fatal("credential Secret must follow the Helm release lifecycle")
				}
				if agent.Spec.Template.Annotations["checksum/agent-secret"] != console.TokenHash(token) {
					t.Fatal("managed token is not included in the pod rollout checksum")
				}
			} else if secret != nil || agent.Spec.Template.Annotations["checksum/agent-secret"] != "" { // pragma: allowlist secret - Kubernetes object comparison
				t.Fatal("external Secret mode must not own or checksum credentials")
			}
			if len(agent.Spec.Template.Spec.Volumes) != 1 || agent.Spec.Template.Spec.Volumes[0].Secret.SecretName != name { // pragma: allowlist secret - Secret name comparison
				t.Fatal("agent does not mount the selected credential Secret")
			}
		})
	}
}

func TestAgentSecretChartRejectsNonStringToken(t *testing.T) {
	values := agentChartValues("", true, "")
	values["agent"].(map[string]any)["secret"].(map[string]any)["token"] = 1234
	if _, err := renderConsoleChart(t, values); err == nil {
		t.Fatal("numeric token accepted; require a string so values are not coerced")
	}
}

func agentChartValues(existing string, create bool, token string) map[string]any {
	return map[string]any{
		"server": map[string]any{"enabled": false},
		"agent": map[string]any{
			"enabled": true, "clusterID": "test", "serverURL": "https://console.example",
			"existingSecret": existing, "secret": map[string]any{"create": create, "token": token},
		},
	}
}

func renderConsoleChart(t *testing.T, values map[string]any) ([]byte, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is required for chart validation (installed in console E2E CI)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "helm", "template", "secret-test", consoleChartPath(), "-f", privateValuesFile(t, values))
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	output, err := cmd.Output()
	if err != nil {
		return diagnostics.Bytes(), err
	}
	return output, nil
}

func consoleChartPath() string {
	return filepath.Join(framework.DefaultRepoRoot(), "charts", "optipod-console")
}

func privateValuesFile(t *testing.T, values any) string {
	t.Helper()
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "values.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeAgentChart(t *testing.T, data []byte) (*corev1.Secret, *appsv1.StatefulSet) {
	t.Helper()
	var secret *corev1.Secret
	var agent *appsv1.StatefulSet
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var kind metav1.TypeMeta
		if err := json.Unmarshal(raw, &kind); err != nil {
			t.Fatal(err)
		}
		switch kind.Kind {
		case "Secret":
			secret = &corev1.Secret{}
			if err := json.Unmarshal(raw, secret); err != nil {
				t.Fatal(err)
			}
		case "StatefulSet":
			agent = &appsv1.StatefulSet{}
			if err := json.Unmarshal(raw, agent); err != nil {
				t.Fatal(err)
			}
		}
	}
	return secret, agent
}
