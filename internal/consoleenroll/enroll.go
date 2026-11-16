// Package consoleenroll exchanges a protected enrollment file for an agent
// credential after verifying the explicitly selected Kubernetes cluster.
package consoleenroll

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/optipod/optipod/internal/console"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Options struct {
	EnrollmentFile, Kubeconfig, Context, TokenOutput string
	AllowHTTP                                        bool
}

type File struct {
	console.IssuedCredential
	ServerURL string `json:"serverURL"`
	KubeUID   string `json:"kubeUID"`
}

type Result struct {
	CredentialID string
	ExpiresAt    time.Time
}

func Run(ctx context.Context, o Options) (*Result, error) {
	if o.EnrollmentFile == "" || o.Kubeconfig == "" || o.Context == "" || o.TokenOutput == "" {
		return nil, fmt.Errorf("enrollment-file, kubeconfig, context and token-output are required")
	}
	f, err := readEnrollment(o.EnrollmentFile, o.AllowHTTP)
	if err != nil {
		return nil, err
	}
	if err = verifyCluster(ctx, o, f.KubeUID); err != nil {
		return nil, err
	}
	// Reserve the destination before consuming the one-time credential. Never
	// replace an existing runtime token, and never put it in stdout or arguments.
	output, err := os.OpenFile(o.TokenOutput, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot create a new private token-output file")
	}
	complete := false
	defer func() {
		_ = output.Close()
		if !complete {
			_ = os.Remove(o.TokenOutput)
		}
	}()
	credential, err := exchange(ctx, f)
	if err != nil {
		return nil, err
	}
	if _, err = output.WriteString(credential.Token); err != nil {
		return nil, fmt.Errorf("cannot save runtime credential; revoke it and issue a new enrollment")
	}
	if err = output.Sync(); err != nil {
		return nil, fmt.Errorf("cannot persist runtime credential; revoke it and issue a new enrollment")
	}
	if err = output.Close(); err != nil {
		return nil, fmt.Errorf("cannot close runtime credential file")
	}
	complete = true
	return &Result{CredentialID: credential.ID, ExpiresAt: credential.ExpiresAt}, nil
}

func readEnrollment(path string, allowHTTP bool) (*File, error) {
	input, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open enrollment file")
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return nil, fmt.Errorf("enrollment file must be a private regular file of at most 64KiB; use chmod 600")
	}
	var f File
	d := json.NewDecoder(io.LimitReader(input, 65537))
	d.DisallowUnknownFields()
	var trailing any
	if d.Decode(&f) != nil || d.Decode(&trailing) != io.EOF || len(f.Token) < 32 || f.ClusterID == "" || f.KubeUID == "" || !f.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("invalid or expired enrollment file; download a new one from the console")
	}
	u, err := url.Parse(f.ServerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("enrollment serverURL must be an HTTPS origin")
	}
	ip := net.ParseIP(u.Hostname())
	local := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && (!allowHTTP || u.Scheme != "http" || !local) {
		return nil, fmt.Errorf("enrollment requires HTTPS; HTTP opt-in is limited to loopback test endpoints")
	}
	return &f, nil
}

func verifyCluster(ctx context.Context, o Options, expectedUID string) error {
	raw, err := clientcmd.LoadFromFile(o.Kubeconfig)
	if err != nil || raw.Contexts[o.Context] == nil {
		return fmt.Errorf("explicit kubeconfig/context could not be loaded")
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, o.Context, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		return fmt.Errorf("invalid explicit Kubernetes context")
	}
	cfg.Timeout = 15 * time.Second
	c, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("cannot initialize Kubernetes client")
	}
	ns, err := c.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot read kube-system UID in the selected context; check connectivity and permissions")
	}
	if string(ns.UID) != expectedUID {
		return fmt.Errorf("selected Kubernetes context does not match the registered cluster UID; enrollment was not consumed")
	}
	return nil
}

func exchange(ctx context.Context, f *File) (*console.IssuedCredential, error) {
	body, _ := json.Marshal(map[string]string{"clusterID": f.ClusterID, "kubeUID": f.KubeUID})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(f.ServerURL, "/")+"/api/enroll", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("invalid enrollment endpoint")
	}
	r.Header.Set("Authorization", "Bearer "+f.Token)
	r.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := c.Do(r)
	if err != nil {
		return nil, fmt.Errorf("enrollment exchange failed; check console connectivity and TLS")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("enrollment rejected (HTTP %d); verify the registration or download a new enrollment file", response.StatusCode)
	}
	var credential console.IssuedCredential
	if json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&credential) != nil || credential.ClusterID != f.ClusterID || len(credential.Token) < 32 || !credential.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("invalid enrollment response; revoke the issued credential before retrying")
	}
	return &credential, nil
}
