package consolee2e

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/optipod/optipod/internal/console"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOnboardingBrowserJourney(t *testing.T) {
	if os.Getenv("OPTIPOD_CONSOLE_UI_E2E") != "1" {
		t.Skip("set OPTIPOD_CONSOLE_UI_E2E=1 after installing hack/console/ui dependencies")
	}
	paths := filepath.SplitList(os.Getenv("OPTIPOD_CONSOLE_E2E_KUBECONFIGS"))
	if len(paths) != 2 {
		t.Fatal("two owned cluster kubeconfigs required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	clusters := ownedClients(t, ctx, paths)
	store := isolatedOnboardingStore(t, ctx)
	server := httptest.NewUnstartedServer(nil)
	bootstrap, admin := uuid.NewString()+uuid.NewString(), uuid.NewString()+uuid.NewString()
	endpoint := "http://" + server.Listener.Addr().String()
	server.Config.Handler = (&console.Server{Store: store, Config: console.Config{PublicURL: endpoint, BootstrapTokenHash: console.TokenHash(bootstrap)}}).Handler()
	server.Start()
	defer server.Close()
	uids := []string{}
	for _, c := range clusters {
		ns, err := c.Kube.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		uids = append(uids, string(ns.UID))
	}
	fixture := privateValuesFile(t, map[string]string{"url": endpoint, "bootstrap": bootstrap, "admin": admin, "uid": uids[0], "peerUID": uids[1]})
	root := filepath.Dir(filepath.Dir(consoleChartPath()))
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "hack/console/ui/onboarding.cjs"))
	cmd.Env = append(os.Environ(), "OPTIPOD_UI_FIXTURE="+fixture)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser E2E failed: %s", output)
	}
	t.Log(string(output))
}
