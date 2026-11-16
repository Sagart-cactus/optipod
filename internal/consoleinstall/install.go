package consoleinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/optipod/optipod/internal/consoleenroll"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type Options struct {
	Kubeconfig, Context, PlanFile, EnrollmentFile, StateDir, OutputDir string
	ConsoleChart, ControllerChart, ControllerImage                     string
	AllowHTTP, Reenroll                                                bool
}

type State struct {
	PlanDigest string `json:"planDigest"`
	TokenFile  string `json:"tokenFile"`
	Phase      string `json:"phase"`
}

func Run(ctx context.Context, o Options) (*Report, error) {
	if err := separateDirectories(o.StateDir, o.OutputDir); err != nil {
		return nil, err
	}
	if o.StateDir == "" {
		return nil, fmt.Errorf("a private state-dir is required")
	}
	if err := PrivateDirectory(o.StateDir); err != nil {
		return nil, err
	}
	p, err := installationPlan(o)
	if err != nil {
		return nil, err
	}
	target, err := OpenTarget(o.Kubeconfig, o.Context)
	if err != nil {
		return nil, err
	}
	if err = VerifyBinding(ctx, target, p); err != nil {
		return nil, err
	}
	state, err := readState(o.StateDir, p)
	if err != nil {
		return nil, err
	}
	preflight, err := preflight(ctx, target, p, o.AllowHTTP, state.TokenFile != "")
	if err != nil {
		return &preflight, err
	}
	data, _ := json.MarshalIndent(preflight, "", "  ")
	if err = writeAtomic(filepath.Join(o.StateDir, "preflight.json"), data); err != nil {
		return &preflight, err
	}
	if !preflight.Ready() {
		return &preflight, fmt.Errorf("preflight failed; follow the diagnostic report before installing")
	}
	if err = verifyHelmRelease(ctx, o, p); err != nil {
		return &preflight, err
	}
	if err = verifyAgentRelease(ctx, target, p); err != nil {
		return &preflight, err
	}
	if state.TokenFile == "" || o.Reenroll {
		if o.EnrollmentFile == "" {
			return &preflight, fmt.Errorf("enrollment-file is required for a new credential")
		}
		if err = verifyEnrollment(o.EnrollmentFile, p, o.AllowHTTP); err != nil {
			return &preflight, err
		}
		state.TokenFile = "runtime-" + uuid.NewString()[:8] + ".token"
		_, err = consoleenroll.Run(ctx, consoleenroll.Options{EnrollmentFile: o.EnrollmentFile, Kubeconfig: o.Kubeconfig, Context: o.Context, TokenOutput: filepath.Join(o.StateDir, state.TokenFile), AllowHTTP: o.AllowHTTP})
		if err != nil {
			return &preflight, err
		}
		state.Phase = "enrolled"
		if err = saveState(o.StateDir, state); err != nil {
			return &preflight, err
		}
	}
	if p.Registration.InstallMethod == "gitops" {
		if o.OutputDir == "" {
			return &preflight, fmt.Errorf("output-dir is required for GitOps manifests")
		}
		if err = Render(ctx, p, o); err != nil {
			return &preflight, err
		}
		state.Phase = "gitops-exported"
	} else {
		if err = installHelm(ctx, target, p, o, state); err != nil {
			return &preflight, err
		}
		state.Phase = "installed"
	}
	return &preflight, saveState(o.StateDir, state)
}

func installationPlan(o Options) (*Plan, error) {
	path := filepath.Join(o.StateDir, "plan.json")
	if o.PlanFile != "" {
		p, err := ReadPlan(o.PlanFile, o.AllowHTTP)
		if err != nil {
			return nil, err
		}
		return p, WritePlan(path, p)
	}
	if _, err := os.Stat(path); err == nil {
		return ReadPlan(path, o.AllowHTTP)
	}
	f, err := consoleenroll.ReadFile(o.EnrollmentFile, o.AllowHTTP)
	if err != nil {
		return nil, err
	}
	p, err := FromEnrollment(f, o.AllowHTTP)
	if err != nil {
		return nil, err
	}
	return p, WritePlan(path, p)
}

func readState(directory string, p *Plan) (*State, error) {
	digest := planDigest(p)
	data, err := os.ReadFile(filepath.Join(directory, "state.json"))
	if os.IsNotExist(err) {
		return &State{PlanDigest: digest, Phase: "planned"}, nil
	}
	if err != nil {
		return nil, err
	}
	var state State
	if json.Unmarshal(data, &state) != nil || state.PlanDigest != digest || state.TokenFile != filepath.Base(state.TokenFile) || state.TokenFile == "." {
		return nil, fmt.Errorf("state does not match this plan; do not reuse a different cluster's state directory")
	}
	return &state, nil
}

func saveState(directory string, state *State) error {
	data, _ := json.Marshal(state)
	return writeAtomic(filepath.Join(directory, "state.json"), data)
}

func verifyAgentRelease(ctx context.Context, t *Target, p *Plan) error {
	i := p.Registration.Installation
	sets, err := t.Kube.AppsV1().StatefulSets(i.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("cannot inspect existing agents")
	}
	for _, set := range sets.Items {
		if set.Name != i.Release+"-agent" {
			continue
		}
		if p.Registration.InstallMethod == "gitops" || set.Annotations["meta.helm.sh/release-name"] != i.Release {
			return fmt.Errorf("existing agent resources will not be adopted; select their existing Helm release or a new deployment")
		}
		for _, c := range set.Spec.Template.Spec.Containers {
			for _, arg := range c.Args {
				if strings.HasPrefix(arg, "--cluster-id=") && arg != "--cluster-id="+p.Registration.ID {
					return fmt.Errorf("existing agent release belongs to another cluster ID; preserve its queue and registration")
				}
			}
		}
	}
	return nil
}

func installHelm(ctx context.Context, t *Target, p *Plan, o Options, state *State) error {
	i := p.Registration.Installation
	if p.Registration.InstallMode == newControllerMode {
		if err := ensureNamespace(ctx, t, i.ControllerNamespace); err != nil {
			return err
		}
		values, err := controllerValues(p, o.ControllerImage)
		if err != nil {
			return err
		}
		if err := helmApply(ctx, o, chart(o.ControllerChart, "optipod"), p.ControllerVersion, i.ControllerRelease, i.ControllerNamespace, values, nil); err != nil {
			return err
		}
	}
	values, err := agentValues(p, false)
	if err != nil {
		return err
	}
	tokenPath := filepath.Join(o.StateDir, state.TokenFile)
	args := []string{"--set-file", "agent.secret.token=" + tokenPath}
	if p.Registration.RequireMTLS {
		args = append(args, "--set-file", "agent.secret.identity="+tokenPath+".identity.json")
	}
	return helmApply(ctx, o, chart(o.ConsoleChart, "optipod-console"), p.ConsoleVersion, i.Release, i.Namespace, values, args)
}

func chart(override, name string) string {
	if override != "" {
		return override
	}
	return "oci://ghcr.io/sagart-cactus/charts/" + name
}

func helmApply(ctx context.Context, o Options, chartPath, version, release, namespace string, values map[string]any, extra []string) error {
	data, _ := json.Marshal(values)
	file, err := os.CreateTemp(o.StateDir, "helm-values-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	args := make([]string, 0, 18+len(extra))
	args = append(args, "upgrade", "--install", release, chartPath, kubeconfigFlag, o.Kubeconfig, contextFlag, o.Context, namespaceFlag, namespace, "--create-namespace", "--version", version, "-f", file.Name(), "--wait", "--timeout", "300s")
	args = append(args, extra...)
	// Helm output can contain rendered Secrets on failure. Never surface it.
	if err = exec.CommandContext(ctx, "helm", args...).Run(); err != nil {
		return fmt.Errorf("helm release %s in %s did not become ready; inspect release status, then rerun with the same state directory", release, namespace)
	}
	return nil
}

func Render(ctx context.Context, p *Plan, o Options) error {
	if err := separateDirectories(o.StateDir, o.OutputDir); err != nil {
		return err
	}
	if err := p.Validate(o.AllowHTTP); err != nil {
		return err
	}
	if o.OutputDir == "" {
		return fmt.Errorf("output-dir is required")
	}
	if err := os.MkdirAll(o.OutputDir, 0755); err != nil {
		return err
	}
	i := p.Registration.Installation
	agent, err := agentValues(p, true)
	if err != nil {
		return err
	}
	if err = renderChart(ctx, o.OutputDir, "agent", chart(o.ConsoleChart, "optipod-console"), p.ConsoleVersion, i.Release, i.Namespace, agent); err != nil {
		return err
	}
	if p.Registration.InstallMode == newControllerMode {
		values, e := controllerValues(p, o.ControllerImage)
		if e != nil {
			return e
		}
		if err = renderChart(ctx, o.OutputDir, "controller", chart(o.ControllerChart, "optipod"), p.ControllerVersion, i.ControllerRelease, i.ControllerNamespace, values); err != nil {
			return err
		}
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	if err = writeAtomic(filepath.Join(o.OutputDir, "plan.json"), data); err != nil {
		return err
	}
	readme := "Apply the rendered namespace manifests before the workload manifests. Supply the referenced agent Secret through your secret manager: keys token and (for mTLS) identity.json. The private credentials remain in the installer state directory and are intentionally absent from this export. Keep the queue PVC across retries, upgrades and re-enrollment. This export never applies or adopts resources and never enables Auto mode. Run preflight against the target context before reconciliation.\n"
	return writeAtomic(filepath.Join(o.OutputDir, "README.txt"), []byte(readme))
}

func renderChart(ctx context.Context, directory, name, chartPath, version, release, namespace string, values map[string]any) error {
	data, err := yaml.Marshal(values)
	if err != nil {
		return err
	}
	file := filepath.Join(directory, name+"-values.yaml")
	if err = writeAtomic(file, data); err != nil {
		return err
	}
	args := []string{"template", release, chartPath, namespaceFlag, namespace, "--version", version, "--include-crds", "-f", file}
	output, err := exec.CommandContext(ctx, "helm", args...).Output()
	if err != nil {
		return fmt.Errorf("cannot render %s chart; verify chart availability and version", name)
	}
	if err = writeAtomic(filepath.Join(directory, name+".yaml"), output); err != nil {
		return err
	}
	ns, _ := yaml.Marshal(map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]string{nameKey: namespace}})
	return writeAtomic(filepath.Join(directory, name+"-namespace.yaml"), ns)
}

func verifyEnrollment(path string, p *Plan, allowHTTP bool) error {
	f, err := consoleenroll.ReadFile(path, allowHTTP)
	if err != nil {
		return err
	}
	if f.Registration == nil || f.ClusterID != p.Registration.ID || f.KubeUID != p.Registration.KubeUID || f.ServerURL != p.ServerURL || f.RequireMTLS != p.Registration.RequireMTLS || !slices.Equal(f.Registration.Namespaces, p.Registration.Namespaces) {
		return fmt.Errorf("enrollment package does not match the saved cluster, scope and endpoint; no credential consumed")
	}
	return nil
}
func separateDirectories(state, output string) error {
	if output == "" || state == "" {
		return nil
	}
	a, err := filepath.Abs(state)
	if err != nil {
		return err
	}
	b, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	// Resolve existing parents as well so symlinks cannot export private state.
	a = resolvePath(a)
	b = resolvePath(b)
	if a == b || strings.HasPrefix(a, b+string(os.PathSeparator)) || strings.HasPrefix(b, a+string(os.PathSeparator)) {
		return fmt.Errorf("GitOps output and private state must be separate, non-overlapping directories")
	}
	return nil
}
func resolvePath(p string) string {
	if actual, err := filepath.EvalSymlinks(p); err == nil {
		return actual
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(resolvePath(parent), filepath.Base(p))
}

// Inspect Helm's release records before mutation; a combined console/agent
// release must never be converted to or uninstalled as an agent-only release.
func verifyHelmRelease(ctx context.Context, o Options, p *Plan) error {
	i := p.Registration.Installation
	args := []string{listVerb, "--deployed", "--failed", "--pending", "--uninstalling", "--uninstalled", "--superseded", "--filter", "^" + i.Release + "$", "--output", "json", kubeconfigFlag, o.Kubeconfig, contextFlag, o.Context, namespaceFlag, i.Namespace}
	output, err := exec.CommandContext(ctx, "helm", args...).Output()
	if err != nil {
		return fmt.Errorf("cannot inspect existing Helm releases")
	}
	var releases []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(output, &releases) != nil {
		return fmt.Errorf("invalid Helm release inventory")
	}
	exists := false
	for _, r := range releases {
		if r.Name == i.Release {
			exists = true
		}
	}
	if !exists {
		return nil
	}
	if p.Registration.InstallMethod != "helm" {
		return fmt.Errorf("GitOps cannot adopt an existing Helm release")
	}
	args = []string{getVerb, "values", i.Release, "--all", "--output", "json", kubeconfigFlag, o.Kubeconfig, contextFlag, o.Context, namespaceFlag, i.Namespace}
	output, err = exec.CommandContext(ctx, "helm", args...).Output()
	if err != nil {
		return fmt.Errorf("cannot inspect Helm release ownership")
	}
	var values struct {
		Server struct {
			Enabled *bool `json:"enabled"`
		} `json:"server"`
		Agent struct {
			Enabled   bool   `json:"enabled"`
			ClusterID string `json:"clusterID"`
		} `json:"agent"`
		PostgreSQL struct {
			Enabled bool `json:"enabled"`
		} `json:"postgresql"`
	}
	if json.Unmarshal(output, &values) != nil || values.Server.Enabled == nil || *values.Server.Enabled || values.PostgreSQL.Enabled || !values.Agent.Enabled || values.Agent.ClusterID != p.Registration.ID {
		return fmt.Errorf("release is not this cluster's dedicated agent release; combined console releases cannot be adopted or removed")
	}
	return nil
}
