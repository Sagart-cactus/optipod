package consolee2e

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleagent"
	"github.com/optipod/optipod/internal/consoleenroll"
	"github.com/optipod/optipod/internal/consoletls"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func TestMTLSEnrollmentRenewalAndRevocation(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("OPTIPOD_CONSOLE_E2E_KUBECONFIGS"))
	if testing.Short() || len(paths) != 2 {
		t.Skip("set two owned Kind kubeconfigs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	clusters := ownedClients(t, ctx, paths)
	store := isolatedOnboardingStore(t, ctx)
	pki := filepath.Join(t.TempDir(), "pki")
	if err := consoletls.Generate(pki, []string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	endpoint := "https://" + server.Listener.Addr().String()
	bootstrap, admin := uuid.NewString()+uuid.NewString(), uuid.NewString()+uuid.NewString()
	tlsConfig := &console.AgentTLSConfig{Listen: server.Listener.Addr().String(), PublicURL: endpoint, ServerCertificateFile: filepath.Join(pki, "server.crt"), ServerKeyFile: filepath.Join(pki, "server.key"), ServerCAFile: filepath.Join(pki, "ca.crt"), ClientCAFile: filepath.Join(pki, "ca.crt"), ClientCAKeyFile: filepath.Join(pki, "ca.key")}
	api := &console.Server{Store: store, Config: console.Config{PublicURL: endpoint, BootstrapTokenHash: console.TokenHash(bootstrap), AgentTLS: tlsConfig}}
	var err error
	server.TLS, err = api.InitAgentTLS()
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = api.Handler()
	server.StartTLS()
	defer server.Close()
	trust, err := consoletls.Client([]byte(api.AgentTrustPEM), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer trust.CloseIdleConnections()
	call := func(method, path, token string, body any, status int, result any) {
		t.Helper()
		onboardingCallClient(t, trust, endpoint, method, path, token, body, status, result)
	}
	call("POST", "/auth/bootstrap", bootstrap, map[string]string{"subject": "mtls-admin", "token": admin}, 204, nil)
	ns := "onboarding-mtls-" + uuid.NewString()[:8]
	_, err = clusters[0].Kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = clusters[0].Kube.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	})
	seedWorkload(t, ctx, clusters[0], ns, 0)
	system, err := clusters[0].Kube.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reg := console.Registration{ID: "mtls-qa", Name: "mTLS QA", KubeUID: string(system.UID), Namespaces: []string{ns}, MetricsSource: "metrics-server", InstallMode: "existing", InstallMethod: "helm"}
	call("POST", "/api/admin/clusters", admin, reg, 201, nil)
	reg.Revision = 1
	call("PUT", "/api/admin/clusters", admin, reg, 409, nil)
	reg.RequireMTLS = true
	call("PUT", "/api/admin/clusters", admin, reg, 200, nil)
	var enrollment console.IssuedCredential
	call("POST", "/api/admin/enrollments", admin, map[string]string{"clusterID": reg.ID}, 200, &enrollment)
	if !enrollment.RequireMTLS || enrollment.ServerURL != endpoint || enrollment.CAPEM == "" {
		t.Fatal("mTLS enrollment configuration missing")
	}
	call("POST", "/api/enroll", enrollment.Token, map[string]string{"clusterID": reg.ID, "kubeUID": reg.KubeUID}, 401, nil)
	file := privateValuesFile(t, consoleenroll.File{IssuedCredential: enrollment, ServerURL: endpoint, KubeUID: reg.KubeUID})
	kube, err := clientcmd.LoadFromFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if _, err = consoleenroll.Run(ctx, consoleenroll.Options{EnrollmentFile: file, Kubeconfig: paths[0], Context: kube.CurrentContext, TokenOutput: tokenFile}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tokenFile + ".identity.json")
	if err != nil {
		t.Fatal(err)
	}
	var identity consoletls.Identity
	if err = json.Unmarshal(data, &identity); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/heartbeat", identity.Token, console.AgentHealth{}, 401, nil)
	identityClient, e := identity.Client()
	if e != nil {
		t.Fatal(e)
	}
	defer identityClient.CloseIdleConnections()
	_, otherCSR, e := consoletls.NewKeyAndCSR()
	if e != nil {
		t.Fatal(e)
	}
	onboardingCallClient(t, identityClient, endpoint, "POST", "/api/renew", identity.Token, map[string]string{"csr": otherCSR}, 401, nil)
	options := consoleagent.Options{Kubeconfig: paths[0], ClusterID: reg.ID, QueueDir: t.TempDir(), URL: endpoint, TokenFile: tokenFile, IdentityFile: tokenFile + ".identity.json", Namespaces: reg.Namespaces, Interval: time.Second, QueueBytes: 16 * 1024 * 1024, Once: true}
	if err = consoleagent.Run(ctx, options); err != nil {
		t.Fatal("mTLS agent collection failed", err)
	}
	// Make renewal due without shortening the valid certificate used for TLS.
	identity.ExpiresAt = time.Now().Add(time.Hour)
	data, _ = json.Marshal(identity)
	if err = os.WriteFile(filepath.Join(options.QueueDir, "identity.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = consoleagent.Run(ctx, options); err != nil {
		t.Fatal("automatic renewal failed", err)
	}
	data, err = os.ReadFile(filepath.Join(options.QueueDir, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var renewed consoletls.Identity
	if json.Unmarshal(data, &renewed) != nil || renewed.Certificate == identity.Certificate || time.Until(renewed.ExpiresAt) < 20*time.Hour {
		t.Fatal("renewed identity did not persist")
	}
	call("POST", "/api/admin/revoke", admin, map[string]string{"id": identity.CredentialID}, 204, nil)
	if err = consoleagent.Run(ctx, options); err == nil {
		t.Fatal("revoked certificate identity uploaded successfully")
	}
	call("POST", "/api/admin/enrollments", admin, map[string]string{"clusterID": reg.ID}, 200, &enrollment)
	fresh := privateValuesFile(t, consoleenroll.File{IssuedCredential: enrollment, ServerURL: endpoint, KubeUID: reg.KubeUID})
	rotateLiveIdentity(t, ctx, options, kube.CurrentContext, fresh, identity.CredentialID)
	page, err := store.List(ctx, "workloads", console.Query{Clusters: []string{reg.ID}})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("revocation lost history", err)
	}
	t.Log("real-cluster mTLS enrollment, token-only rejection, renewal, live external Secret rotation, queue continuity and immediate revocation passed")
}

func rotateLiveIdentity(t *testing.T, ctx context.Context, options consoleagent.Options, contextName, fresh, previousID string) {
	t.Helper()
	running, cancelRunning := context.WithCancel(ctx)
	defer cancelRunning()
	options.Once = false
	done := make(chan error, 1)
	go func() { done <- consoleagent.Run(running, options) }()
	replacement := filepath.Join(t.TempDir(), "replacement")
	if _, e := consoleenroll.Run(ctx, consoleenroll.Options{EnrollmentFile: fresh, Kubeconfig: options.Kubeconfig, Context: contextName, TokenOutput: replacement}); e != nil {
		cancelRunning()
		t.Fatal(e)
	}
	for _, suffix := range []string{"", ".identity.json"} {
		data, e := os.ReadFile(replacement + suffix)
		if e != nil {
			cancelRunning()
			t.Fatal(e)
		}
		if e = os.WriteFile(options.TokenFile+suffix, data, 0600); e != nil {
			cancelRunning()
			t.Fatal(e)
		}
	}
	wait(t, ctx, "external Secret identity reload", func() bool {
		data, e := os.ReadFile(filepath.Join(options.QueueDir, "identity.json"))
		if e != nil {
			return false
		}
		var current consoletls.Identity
		if json.Unmarshal(data, &current) != nil {
			return false
		}
		return current.CredentialID != previousID
	})
	cancelRunning()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	options.Once = true
	if e := consoleagent.Run(ctx, options); e != nil {
		t.Fatal("replacement identity could not drain retained queue", e)
	}
}
