/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBuildHTTPClient_NoAuth(t *testing.T) {
	config := PrometheusConfig{
		URL: "http://prometheus:9090",
		Auth: PrometheusAuth{
			Type: "none",
		},
		Timeout: 30 * time.Second,
	}

	client, err := buildHTTPClient(config)
	if err != nil {
		t.Fatalf("Failed to build HTTP client: %v", err)
	}

	if client.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", client.Timeout)
	}
}

func TestBuildHTTPClient_BasicAuth(t *testing.T) {
	config := PrometheusConfig{
		URL: "http://prometheus:9090",
		Auth: PrometheusAuth{
			Type:     "basic",
			Username: "testuser",
			Password: "testpass",
		},
	}

	client, err := buildHTTPClient(config)
	if err != nil {
		t.Fatalf("Failed to build HTTP client: %v", err)
	}

	// Test that basic auth is applied
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Error("Basic auth not present")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if username != "testuser" || password != "testpass" { // pragma: allowlist secret
			t.Errorf("Expected testuser:testpass, got %s:%s", username, password)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestBuildHTTPClient_BearerToken(t *testing.T) {
	config := PrometheusConfig{
		URL: "http://prometheus:9090",
		Auth: PrometheusAuth{
			Type:        "bearer",
			BearerToken: "test-token-123",
		},
	}

	client, err := buildHTTPClient(config)
	if err != nil {
		t.Fatalf("Failed to build HTTP client: %v", err)
	}

	// Test that bearer token is applied
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-123" {
			t.Errorf("Expected 'Bearer test-token-123', got '%s'", auth)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestBuildHTTPClient_MissingCredentials(t *testing.T) {
	tests := []struct {
		name   string
		config PrometheusConfig
	}{
		{
			name: "basic auth without username",
			config: PrometheusConfig{
				Auth: PrometheusAuth{
					Type:     "basic",
					Password: "pass",
				},
			},
		},
		{
			name: "basic auth without password",
			config: PrometheusConfig{
				Auth: PrometheusAuth{
					Type:     "basic",
					Username: "user",
				},
			},
		},
		{
			name: "bearer auth without token",
			config: PrometheusConfig{
				Auth: PrometheusAuth{
					Type: "bearer",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildHTTPClient(tt.config)
			if err == nil {
				t.Error("Expected error for missing credentials, got nil")
			}
		})
	}
}

func TestBuildHTTPClient_UnsupportedAuthType(t *testing.T) {
	config := PrometheusConfig{
		Auth: PrometheusAuth{
			Type: "oauth2",
		},
	}

	_, err := buildHTTPClient(config)
	if err == nil {
		t.Error("Expected error for unsupported auth type, got nil")
	}
}

func TestBuildHTTPClient_DefaultTimeout(t *testing.T) {
	config := PrometheusConfig{
		URL: "http://prometheus:9090",
	}

	client, err := buildHTTPClient(config)
	if err != nil {
		t.Fatalf("Failed to build HTTP client: %v", err)
	}

	if client.Timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got %v", client.Timeout)
	}
}
