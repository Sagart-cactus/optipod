package framework

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"
)

// Phase records one setup phase's wall time for later timing diagnostics.
type Phase struct {
	Name     string
	Duration time.Duration
}

// Installation captures the single build/load/install cycle's evidence: which
// images were built and loaded, where the temporary rendered manifest lives,
// and which webhook CA the API server trusts.
type Installation struct {
	Rendered         *RenderedManifests
	ControllerImage  *BuiltImage
	FixtureImage     *BuiltImage
	WorkloadImageRef string
	WebhookCert      *WebhookServingCert
	FixtureURL       string
}

// Environment is the fully prepared per-suite harness state: an owned cluster
// with the freshly built controller and deterministic metrics fixture
// installed, typed clients bound to the private kubeconfig, and per-phase
// timings. Specs use Environment.Factory for per-spec unique fixtures and
// Environment.Fixture to control deterministic metric inputs.
type Environment struct {
	Cluster      *Cluster
	Clients      *Clients
	Factory      *Factory
	Fixture      *MetricsFixtureClient
	Installation *Installation
	Phases       []Phase
}

// EnvironmentOptions configures SetupEnvironment.
type EnvironmentOptions struct {
	// Cluster configures cluster creation (KEEP_CLUSTER etc.).
	Cluster ClusterOptions
	// RepoRoot is the checkout root; defaults to the repository containing
	// this source file so the harness works from any working directory.
	RepoRoot string
	// SkipPreflight is used only by tests that inject fakes.
	SkipPreflight bool
	// ReconciliationInterval overrides the deployed controller's
	// reconciliation interval (default 10s in the rendered config).
	ReconciliationInterval string
	// Preflighter overrides the default preflight checks (tests).
	Preflighter *Preflighter
}

// DefaultRepoRoot returns the repository root derived from this source file's
// location (test/e2e/framework), independent of the process working directory.
func DefaultRepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	if err != nil {
		return filepath.Join(filepath.Dir(file), "..", "..", "..")
	}
	return abs
}

// SetupEnvironment runs the whole suite-owned setup exactly once: preflight,
// owned cluster creation, one controller+fixture image build, one load, one
// temporary render, one install, and bounded readiness/identity verification.
// On any failure after the cluster exists, the owned cluster is torn down with
// an independent bounded context unless retention was requested.
func SetupEnvironment(ctx context.Context, opts EnvironmentOptions) (*Environment, error) {
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot = DefaultRepoRoot()
	}

	env := &Environment{}
	step := func(name string, fn func() error) error {
		start := time.Now()
		err := fn()
		env.Phases = append(env.Phases, Phase{Name: name, Duration: time.Since(start)})
		if err != nil {
			return fmt.Errorf("phase %s: %w", name, err)
		}
		return nil
	}

	if err := step("preflight", func() error {
		if opts.SkipPreflight {
			return nil
		}
		preflighter := opts.Preflighter
		if preflighter == nil {
			preflighter = NewPreflighter()
		}
		return preflighter.Check(ctx)
	}); err != nil {
		return nil, err
	}

	var cluster *Cluster
	if err := step("cluster-create", func() error {
		var err error
		cluster, err = CreateCluster(ctx, opts.Cluster)
		return err
	}); err != nil {
		return nil, err
	}
	env.Cluster = cluster
	// From here on, failure tears down the owned cluster (unless retained).
	fail := func(err error) (*Environment, error) {
		cctx, cancel := CleanupContext()
		defer cancel()
		if cleanupErr := cluster.Delete(cctx); cleanupErr != nil && !cluster.Keep() {
			return nil, fmt.Errorf("%w (additionally, cluster cleanup failed: %v)", err, cleanupErr)
		}
		return nil, err
	}

	if err := step("clients", func() error {
		restConfig, err := cluster.RESTConfig()
		if err != nil {
			return err
		}
		clients, err := NewClients(restConfig)
		if err != nil {
			return err
		}
		env.Clients = clients
		return nil
	}); err != nil {
		return fail(err)
	}

	builder := NewImageBuilder(repoRoot)
	installation := &Installation{FixtureURL: FixtureServiceURL(), WorkloadImageRef: WorkloadImage()}
	if err := step("image-build", func() error {
		controllerImage, err := builder.BuildControllerImage(ctx)
		if err != nil {
			return err
		}
		installation.ControllerImage = controllerImage
		fixtureImage, err := builder.BuildFixtureImage(ctx)
		if err != nil {
			return err
		}
		installation.FixtureImage = fixtureImage
		return nil
	}); err != nil {
		return fail(err)
	}

	// Only the two locally built images are loaded. The digest-pinned workload
	// image (pause) is a multi-arch manifest list, which kind cannot load from
	// the local store; pods reference it by digest and the node's containerd
	// pulls exactly that content.
	if err := step("image-load", func() error {
		controllerNodeID, err := builder.LoadImage(ctx, cluster.Name, installation.ControllerImage.Ref)
		if err != nil {
			return err
		}
		installation.ControllerImage.NodeID = controllerNodeID
		fixtureNodeID, err := builder.LoadImage(ctx, cluster.Name, installation.FixtureImage.Ref)
		if err != nil {
			return err
		}
		installation.FixtureImage.NodeID = fixtureNodeID
		return nil
	}); err != nil {
		return fail(err)
	}

	if err := step("render", func() error {
		cert, err := GenerateWebhookServingCert(WebhookServiceName, SystemNamespace)
		if err != nil {
			return err
		}
		installation.WebhookCert = cert
		rendered, err := RenderManifests(ctx, RenderOptions{
			RepoRoot:               repoRoot,
			ControllerImageRef:     installation.ControllerImage.Ref,
			FixtureImageRef:        installation.FixtureImage.Ref,
			WebhookCAPEM:           cert.CAPEM,
			ReconciliationInterval: opts.ReconciliationInterval,
		})
		if err != nil {
			return err
		}
		installation.Rendered = rendered
		return nil
	}); err != nil {
		return fail(err)
	}

	if err := step("install-apply", func() error {
		return Install(ctx, cluster, env.Clients, installation.Rendered, installation.WebhookCert, InstallOptions{})
	}); err != nil {
		return fail(err)
	}

	if err := step("await-ready", func() error {
		if err := AwaitReady(ctx, env.Clients, installation.WebhookCert, defaultAwaitTimeout); err != nil {
			return err
		}
		return VerifyControllerIdentity(ctx, env.Clients, installation.ControllerImage)
	}); err != nil {
		return fail(err)
	}

	env.Installation = installation
	env.Factory = NewFactory(env.Clients)
	env.Fixture = NewMetricsFixtureClient(env.Clients)
	return env, nil
}

// Teardown removes the owned cluster (unless retained) with a bounded context
// independent of any cancelled run context.
func (e *Environment) Teardown() error {
	cctx, cancel := CleanupContext()
	defer cancel()
	return e.Cluster.Delete(cctx)
}
