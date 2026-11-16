package consoleenroll

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/optipod/optipod/internal/console"
)

func TestEnrollmentFileProtectionAndTransport(t *testing.T) {
	for _, tc := range []struct {
		name, origin              string
		mode                      os.FileMode
		expired, allowHTTP, valid bool
	}{
		{name: "private HTTPS", origin: "https://console.example", mode: 0600, valid: true},
		{name: "public file", origin: "https://console.example", mode: 0644},
		{name: "expired", origin: "https://console.example", mode: 0600, expired: true},
		{name: "HTTP", origin: "http://console.example", mode: 0600},
		{name: "HTTP nonlocal override", origin: "http://console.example", mode: 0600, allowHTTP: true},
		{name: "loopback opt-in", origin: "http://127.0.0.1:8766", mode: 0600, allowHTTP: true, valid: true},
		{name: "userinfo", origin: "https://user@console.example", mode: 0600},
		{name: "query", origin: "https://console.example?token=example", mode: 0600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expires := time.Now().Add(time.Hour)
			if tc.expired {
				expires = time.Now().Add(-time.Hour)
			}
			f := File{IssuedCredential: console.IssuedCredential{ClusterID: "qa", Token: strings.Repeat("a", 40), ExpiresAt: expires}, ServerURL: tc.origin, KubeUID: "uid"}
			data, err := json.Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "enrollment.json")
			if err = os.WriteFile(path, data, tc.mode); err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			_, err = readEnrollment(path, tc.allowHTTP)
			if (err == nil) != tc.valid {
				t.Fatal("unexpected enrollment-file validation result")
			}
		})
	}
}

func TestEnrollmentDoesNotFollowRedirectsOrExposeErrorBodies(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("enrollment followed a redirect with credential material")
		w.WriteHeader(200)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	f := &File{ServerURL: origin.URL, KubeUID: "uid", IssuedCredential: console.IssuedCredential{Token: strings.Repeat("a", 40), ClusterID: "qa"}}
	if _, err := exchange(context.Background(), f); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatal("redirect was not rejected")
	}
	leak := "sensitive-server-response"
	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, leak, http.StatusUnauthorized)
	}))
	defer failure.Close()
	f.ServerURL = failure.URL
	if _, err := exchange(context.Background(), f); err == nil || strings.Contains(err.Error(), leak) {
		t.Fatal("credential exchange leaked an untrusted error body")
	}
}
