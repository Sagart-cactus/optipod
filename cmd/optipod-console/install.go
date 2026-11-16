package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/optipod/optipod/internal/consoleenroll"
	"github.com/optipod/optipod/internal/consoleinstall"
)

func onboarding(ctx context.Context, command string, args []string) error {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	var o consoleinstall.Options
	f.StringVar(&o.Kubeconfig, "kubeconfig", "", "explicit target kubeconfig; never uploaded")
	f.StringVar(&o.Context, "context", "", "explicit target Kubernetes context")
	f.StringVar(&o.PlanFile, "plan-file", "", "non-secret installation plan")
	f.StringVar(&o.EnrollmentFile, "enrollment-file", "", "private, one-use enrollment package")
	f.StringVar(&o.StateDir, "state-dir", "", "private directory retained across retries and upgrades")
	f.StringVar(&o.OutputDir, "output-dir", "", "separate directory for secret-free GitOps output")
	f.StringVar(&o.ConsoleChart, "console-chart", "", "optional local console chart or OCI reference")
	f.StringVar(&o.ControllerChart, "controller-chart", "", "optional local controller chart or OCI reference")
	f.StringVar(&o.ControllerImage, "controller-image", "", "optional pinned controller image in a private registry")
	f.BoolVar(&o.AllowHTTP, "allow-insecure-http", false, "isolated local testing only")
	f.BoolVar(&o.Reenroll, "reenroll", false, "replace the runtime identity using a fresh enrollment, retaining the queue")
	if err := f.Parse(args); err != nil {
		return err
	}
	if command == "onboard" {
		report, err := consoleinstall.Run(ctx, o)
		if report != nil {
			_ = json.NewEncoder(os.Stdout).Encode(report)
		}
		if err != nil {
			return err
		}
		fmt.Println("Installation state saved. Check inventory and agent health in the console.")
		return nil
	}
	if command == "plan" {
		if o.PlanFile == "" {
			return fmt.Errorf("plan-file is required for output")
		}
		enrollment, err := consoleenroll.ReadFile(o.EnrollmentFile, o.AllowHTTP)
		if err != nil {
			return err
		}
		p, err := consoleinstall.FromEnrollment(enrollment, o.AllowHTTP)
		if err != nil {
			return err
		}
		return consoleinstall.WritePlan(o.PlanFile, p)
	}
	p, err := consoleinstall.ReadPlan(o.PlanFile, o.AllowHTTP)
	if err != nil {
		return err
	}
	if command == "render" {
		return consoleinstall.Render(ctx, p, o)
	}
	target, err := consoleinstall.OpenTarget(o.Kubeconfig, o.Context)
	if err != nil {
		return err
	}
	report, err := consoleinstall.Preflight(ctx, target, p, o.AllowHTTP)
	_ = json.NewEncoder(os.Stdout).Encode(report)
	if err != nil {
		return err
	}
	if !report.Ready() {
		return fmt.Errorf("preflight failed; resolve the reported checks")
	}
	return nil
}

func probe(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("probe", flag.ContinueOnError)
	var o consoleinstall.ProbeOptions
	f.StringVar(&o.ServerURL, "server-url", "", "console origin")
	ca := f.String("ca-file", "", "public console CA bundle")
	f.StringVar(&o.MetricsSource, "metrics-source", "metrics-server", "metrics-server or prometheus")
	f.StringVar(&o.PrometheusURL, "prometheus-url", "", "Prometheus base URL")
	f.StringVar(&o.PrometheusAuth, "prometheus-auth", "none", "none, bearer or basic")
	f.StringVar(&o.PrometheusAuthDir, "prometheus-auth-dir", "", "mounted authentication Secret")
	f.StringVar(&o.PrometheusCAFile, "prometheus-ca-file", "", "public Prometheus CA")
	f.StringVar(&o.VolumePath, "volume-path", "", "mounted queue probe volume")
	names := f.String("namespaces", "", "comma-separated observed namespaces")
	f.BoolVar(&o.AllowHTTP, "allow-insecure-http", false, "isolated local testing only")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *ca != "" {
		data, err := os.ReadFile(*ca)
		if err != nil {
			return fmt.Errorf("cannot read console CA")
		}
		o.CAPEM = string(data)
	}
	if *names != "" {
		o.Namespaces = strings.Split(*names, ",")
	}
	r := consoleinstall.Probe(ctx, o)
	if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
		return err
	}
	if !r.Ready() {
		return fmt.Errorf("in-cluster preflight failed")
	}
	return nil
}
