// Package consoleinstall provides explicit-context, resumable console onboarding.
package consoleinstall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleenroll"
	"github.com/optipod/optipod/internal/consoleversion"
	"k8s.io/apimachinery/pkg/util/validation"
)

const PlanVersion = "optipod.io/onboarding-v1"
const ControllerVersion = "1.6.0-console.1"

type Plan struct {
	APIVersion         string               `json:"apiVersion"`
	Registration       console.Registration `json:"registration"`
	ServerURL          string               `json:"serverURL"`
	ConsoleURL         string               `json:"consoleURL"`
	CAPEM              string               `json:"caPEM,omitempty"`
	ConsoleVersion     string               `json:"consoleVersion"`
	ControllerVersion  string               `json:"controllerVersion"`
	Image              string               `json:"image"`
	ImagePullSecrets   []string             `json:"imagePullSecrets,omitempty"`
	ConsoleCASecret    string               `json:"consoleCASecret,omitempty"`
	ProxySecret        string               `json:"proxySecret,omitempty"`
	PrometheusCASecret string               `json:"prometheusCASecret,omitempty"`
	PrometheusSecret   string               `json:"prometheusSecret,omitempty"`
	PrometheusAuth     string               `json:"prometheusAuth,omitempty"`
	EnableWebhook      bool                 `json:"enableWebhook"`
}

func FromEnrollment(f *consoleenroll.File, allowHTTP bool) (*Plan, error) {
	if f.Registration == nil {
		return nil, fmt.Errorf("download a current enrollment package containing installation preferences")
	}
	r := *f.Registration
	if r.ID != f.ClusterID || r.KubeUID != f.KubeUID {
		return nil, fmt.Errorf("enrollment registration binding does not match")
	}
	if r.Installation.Namespace == "" {
		r.Installation.Namespace = "optipod-system"
	}
	if r.Installation.Release == "" {
		r.Installation.Release = "optipod-console"
	}
	if r.Installation.ControllerNamespace == "" {
		r.Installation.ControllerNamespace = "optipod-system"
	}
	if r.Installation.ControllerRelease == "" {
		r.Installation.ControllerRelease = optimizerName
	}
	r.RequireMTLS = f.RequireMTLS
	p := &Plan{APIVersion: PlanVersion, Registration: r, ServerURL: f.ServerURL, ConsoleURL: f.ConsoleURL, CAPEM: f.CAPEM, ConsoleVersion: consoleversion.ChartVersion, ControllerVersion: ControllerVersion, Image: "ghcr.io/sagart-cactus/optipod-console:" + consoleversion.ChartVersion}
	return p, p.Validate(allowHTTP)
}

func (p Plan) Validate(allowHTTP bool) error {
	if p.APIVersion != PlanVersion || p.ConsoleVersion != consoleversion.ChartVersion || p.ControllerVersion != ControllerVersion {
		return fmt.Errorf("plan version is incompatible with this installer; regenerate using the matching console release")
	}
	if err := p.Registration.Validate(); err != nil {
		return err
	}
	i := p.Registration.Installation
	if i.Namespace == "" || i.Release == "" || i.ControllerNamespace == "" || i.ControllerRelease == "" || p.Image == "" {
		return fmt.Errorf("plan requires explicit namespaces, releases and versioned image")
	}
	if err := p.validateEndpoint(allowHTTP); err != nil {
		return err
	}
	for _, name := range append(append([]string{}, p.ImagePullSecrets...), p.ProxySecret, p.PrometheusSecret, p.PrometheusCASecret, p.ConsoleCASecret) {
		if name != "" && len(validation.IsDNS1123Subdomain(name)) > 0 {
			return fmt.Errorf("invalid Secret reference")
		}
	}
	if p.PrometheusAuth != "" && p.PrometheusAuth != "none" && p.PrometheusAuth != bearerAuth && p.PrometheusAuth != basicAuth {
		return fmt.Errorf("invalid Prometheus authentication type")
	}
	if (p.PrometheusAuth == bearerAuth || p.PrometheusAuth == basicAuth) && p.PrometheusSecret == "" {
		return fmt.Errorf("prometheus authentication requires an existing Secret reference")
	}
	if _, err := imageValues(p.Image); err != nil {
		return err
	}
	return nil
}

func ReadPlan(path string, allowHTTP bool) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 65536 {
		return nil, fmt.Errorf("plan exceeds 64KiB")
	}
	var plan Plan
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&plan) != nil || d.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("invalid installation plan")
	}
	return &plan, plan.Validate(allowHTTP)
}

func WritePlan(path string, p *Plan) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		prior, e := os.ReadFile(path)
		if e == nil && bytes.Equal(data, prior) {
			return nil
		}
		return fmt.Errorf("existing plan differs; use a new output file")
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err = file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func PrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("state directory must be a private directory; use chmod 700")
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".onboarding-")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (p Plan) validateEndpoint(allowHTTP bool) error {
	u, err := url.Parse(p.ServerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != httpsScheme && (!allowHTTP || u.Scheme != httpScheme)) {
		return fmt.Errorf("console endpoint must be an HTTPS origin")
	}
	if p.Registration.RequireMTLS && u.Scheme != httpsScheme {
		return fmt.Errorf("mTLS plans require HTTPS")
	}
	return nil
}
