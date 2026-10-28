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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

// PrometheusConfig contains configuration for connecting to Prometheus.
type PrometheusConfig struct {
	URL string

	// Authentication
	Auth PrometheusAuth

	// TLS Configuration
	TLS PrometheusTLS

	// HTTP Client Settings
	Timeout time.Duration
}

// PrometheusAuth contains authentication configuration.
type PrometheusAuth struct {
	Type string // "none", "basic", "bearer"

	// Basic Auth
	Username string
	Password string

	// Bearer Token
	BearerToken string
}

// PrometheusTLS contains TLS configuration.
type PrometheusTLS struct {
	Enabled            bool
	InsecureSkipVerify bool
	CAFile             string
	CertFile           string
	KeyFile            string
}

// buildHTTPClient creates an HTTP client with authentication and TLS configuration.
func buildHTTPClient(config PrometheusConfig) (*http.Client, error) {
	// Build TLS config
	tlsConfig, err := buildTLSConfig(config.TLS)
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config: %w", err)
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	// Wrap transport with auth
	var rt http.RoundTripper = transport

	switch config.Auth.Type {
	case "basic":
		if config.Auth.Username == "" || config.Auth.Password == "" {
			return nil, fmt.Errorf("username and password required for basic auth")
		}
		rt = &basicAuthTransport{
			Transport: transport,
			Username:  config.Auth.Username,
			Password:  config.Auth.Password,
		}
	case "bearer":
		if config.Auth.BearerToken == "" {
			return nil, fmt.Errorf("bearer token required for bearer auth")
		}
		rt = &bearerTokenTransport{
			Transport: transport,
			Token:     config.Auth.BearerToken,
		}
	case "none", "":
		// No authentication
	default:
		return nil, fmt.Errorf("unsupported auth type: %s", config.Auth.Type)
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &http.Client{
		Transport: rt,
		Timeout:   timeout,
	}, nil
}

// buildTLSConfig creates a TLS configuration from the provided settings.
func buildTLSConfig(config PrometheusTLS) (*tls.Config, error) {
	if !config.Enabled {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.InsecureSkipVerify,
	}

	// Load CA certificate if provided
	if config.CAFile != "" {
		caCert, err := os.ReadFile(config.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// Load client certificate if provided
	if config.CertFile != "" && config.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// basicAuthTransport adds basic authentication to HTTP requests.
type basicAuthTransport struct {
	Transport http.RoundTripper
	Username  string
	Password  string
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(t.Username, t.Password)
	return t.Transport.RoundTrip(req)
}

// bearerTokenTransport adds bearer token authentication to HTTP requests.
type bearerTokenTransport struct {
	Transport http.RoundTripper
	Token     string
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.Token)
	return t.Transport.RoundTrip(req)
}
