package report

import (
	"net/http/httptest"
	"testing"
)

func TestReadOnlyLoopbackBoundary(t *testing.T) {
	h := (&Collector{}).Handler("127.0.0.1:8080")
	for _, tc := range []struct {
		method, host, origin, path string
		want                       int
	}{{"POST", "127.0.0.1:8080", "", "/", 405}, {"GET", "evil.example:8080", "", "/", 403}, {"GET", "127.0.0.1:8080", "https://evil.example", "/", 403}, {"GET", "127.0.0.1:8080", "", "/", 200}, {"GET", "127.0.0.1:8080", "", "/api/history", 400}} {
		r := httptest.NewRequest(tc.method, "http://"+tc.host+tc.path, nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v got %d", tc, w.Code)
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
}
