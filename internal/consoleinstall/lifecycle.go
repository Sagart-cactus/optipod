package consoleinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/optipod/optipod/internal/consoletls"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AdminClient struct {
	URL, TokenFile, CAFile string
	AllowHTTP              bool
}

func (a AdminClient) Post(ctx context.Context, path string, input any) error {
	u, err := url.Parse(a.URL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != httpsScheme && (!a.AllowHTTP || u.Scheme != httpScheme)) {
		return fmt.Errorf("administration requires an HTTPS console origin")
	}
	info, err := os.Lstat(a.TokenFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return fmt.Errorf("admin-token-file must be a private regular file (0600)")
	}
	token, err := os.ReadFile(a.TokenFile)
	if err != nil {
		return err
	}
	token = bytes.TrimSpace(token)
	if len(token) < 32 {
		return fmt.Errorf("invalid administrator token file")
	}
	var ca []byte
	if a.CAFile != "" {
		ca, err = os.ReadFile(a.CAFile)
		if err != nil {
			return err
		}
	}
	client, err := consoletls.Client(ca, nil, nil)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(a.URL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("console administration unavailable; check routing and CA trust")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return fmt.Errorf("console administration rejected (HTTP %d); verify grants, cluster state and input", response.StatusCode)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 65536))
	return err
}

func Uninstall(ctx context.Context, o Options, a AdminClient) error {
	p, err := ReadPlan(o.PlanFile, o.AllowHTTP)
	if err != nil {
		return err
	}
	t, err := OpenTarget(o.Kubeconfig, o.Context)
	if err != nil {
		return err
	}
	if err = VerifyBinding(ctx, t, p); err != nil {
		return err
	}
	if p.Registration.InstallMethod != "helm" {
		return fmt.Errorf("remove the agent manifests from your GitOps source and disconnect the cluster in the console; retain its queue PVC")
	}
	if err = verifyHelmRelease(ctx, o, p); err != nil {
		return err
	}
	if err = verifyAgentRelease(ctx, t, p); err != nil {
		return err
	}
	i := p.Registration.Installation
	// Revoke first; if local uninstall is interrupted, repeating this command is safe.
	if a.URL == "" {
		a.URL = p.ConsoleURL
	}
	if err = a.Post(ctx, "/api/admin/connection", map[string]any{"id": p.Registration.ID, "connected": false}); err != nil {
		return err
	}
	_, err = t.Kube.AppsV1().StatefulSets(i.Namespace).Get(ctx, i.Release+"-agent", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot inspect agent release")
	}
	args := []string{"uninstall", i.Release, kubeconfigFlag, o.Kubeconfig, contextFlag, o.Context, namespaceFlag, i.Namespace, "--wait", "--timeout", "180s"}
	if err = exec.CommandContext(ctx, "helm", args...).Run(); err != nil {
		return fmt.Errorf("agent uninstall incomplete; credentials are revoked, retry with the same plan")
	}
	return nil
}
