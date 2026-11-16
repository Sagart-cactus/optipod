package consoleagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/optipod/optipod/internal/consoletls"
)

func newUploader(o Options) (*Uploader, *consoletls.Identity, error) {
	u := &Uploader{URL: o.URL, TokenFile: o.TokenFile, AllowHTTP: o.AllowHTTP}
	if err := u.validateURL(); err != nil {
		return nil, nil, err
	}
	identity, err := prepareIdentity(o)
	if err != nil {
		return nil, nil, err
	}
	if identity != nil {
		if o.AllowHTTP {
			return nil, nil, fmt.Errorf("mTLS identity requires HTTPS")
		}
		u.Client, err = identity.Client()
	} else if o.CAFile != "" {
		var data []byte
		data, err = os.ReadFile(o.CAFile)
		if err == nil {
			u.Client, err = consoletls.Client(data, nil, nil)
		}
	}
	return u, identity, err
}

func prepareIdentity(o Options) (*consoletls.Identity, error) {
	if o.IdentityFile == "" {
		return nil, nil
	}
	base, err := loadIdentity(o.IdentityFile)
	if err != nil {
		return nil, err
	}
	token, err := os.ReadFile(o.TokenFile)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(token)) != base.Token {
		return nil, fmt.Errorf("identity and token Secret do not match")
	}
	path := filepath.Join(o.QueueDir, "identity.json")
	if current, err := loadIdentity(path); err == nil && current.CredentialID == base.CredentialID {
		if current.Token != base.Token {
			return nil, fmt.Errorf("persisted identity conflicts with credential")
		}
		return current, nil
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	data, _ := json.Marshal(base)
	if err = atomicWrite(path, data); err != nil {
		return nil, err
	}
	return base, nil
}

func loadIdentity(path string) (*consoletls.Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var identity consoletls.Identity
	if len(data) > 65536 || json.Unmarshal(data, &identity) != nil || identity.CredentialID == "" || len(identity.Token) < 32 {
		return nil, fmt.Errorf("invalid private agent identity")
	}
	if _, err = identity.Client(); err != nil {
		return nil, err
	}
	return &identity, nil
}

func renewIdentity(ctx context.Context, u *Uploader, q *Queue, identity *consoletls.Identity) (*consoletls.Identity, error) {
	if identity == nil || time.Until(identity.ExpiresAt) > 8*time.Hour {
		return identity, nil
	}
	csr, err := consoletls.CSRForKey([]byte(identity.PrivateKey))
	if err != nil {
		return identity, err
	}
	data, _ := json.Marshal(map[string]string{"csr": csr})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(u.URL, "/")+"/api/renew", bytes.NewReader(data))
	if err != nil {
		return identity, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+identity.Token)
	response, err := u.Client.Do(r)
	if err != nil {
		return identity, fmt.Errorf("client certificate renewal unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	var result struct {
		Certificate string    `json:"certificate"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result) != nil || !result.ExpiresAt.After(identity.ExpiresAt) {
		return identity, fmt.Errorf("client certificate renewal rejected")
	}
	next := *identity
	next.Certificate = result.Certificate
	next.ExpiresAt = result.ExpiresAt
	c, err := next.Client()
	if err != nil {
		return identity, err
	}
	data, _ = json.Marshal(next)
	if err = atomicWrite(filepath.Join(q.Dir, "identity.json"), data); err != nil {
		return identity, err
	}
	u.Client.CloseIdleConnections()
	u.Client = c
	return &next, nil
}

// refreshIdentity observes projected Secret rotation without replacing the queue.
func refreshIdentity(o Options, u *Uploader, current *consoletls.Identity) (*consoletls.Identity, error) {
	if o.IdentityFile == "" {
		return current, nil
	}
	next, err := prepareIdentity(o)
	if err != nil {
		return current, err
	}
	if current != nil && current.CredentialID == next.CredentialID && current.Certificate == next.Certificate {
		return current, nil
	}
	client, err := next.Client()
	if err != nil {
		return current, err
	}
	if u.Client != nil {
		u.Client.CloseIdleConnections()
	}
	u.Client = client
	return next, nil
}
