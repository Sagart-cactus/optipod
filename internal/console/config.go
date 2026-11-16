package console

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
)

const RoleAdmin = "admin"
const RoleViewer = "viewer"
const schemeHTTPS = "https"
const savedKey = "saved"

const credentialIDKey = "credentialID"

type AgentCredential struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TokenHash string `json:"tokenHash"`
}

type Reader struct {
	NamespaceScopes map[string][]string `json:"-"`
	Subject         string              `json:"subject"`
	Role            string              `json:"role,omitempty"`
	TokenHash       string              `json:"tokenHash,omitempty"`
	Clusters        []string            `json:"clusters"`
	Namespaces      []string            `json:"namespaces,omitempty"`
}

type OIDCConfig struct {
	Issuer           string   `json:"issuer"`
	ClientID         string   `json:"clientID"`
	ClientSecretFile string   `json:"clientSecretFile,omitempty"`
	GroupsClaim      string   `json:"groupsClaim,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
}

type Config struct {
	BootstrapTokenHash string            `json:"bootstrapTokenHash,omitempty"`
	PublicURL          string            `json:"publicURL"`
	Agents             []AgentCredential `json:"agents"`
	Readers            []Reader          `json:"readers"`
	OIDC               *OIDCConfig       `json:"oidc,omitempty"`
	AgentTLS           *AgentTLSConfig   `json:"agentTLS,omitempty"`
}

func TokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer func() { _ = f.Close() }()
	var c Config
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if err := c.validateAgentTLS(); err != nil {
		return err
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("publicURL must be an absolute origin")
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != schemeHTTPS && (u.Scheme != "http" || !loopback) {
		return fmt.Errorf("publicURL requires HTTPS except on loopback")
	}
	validHash := regexp.MustCompile(`^[a-f0-9]{64}$`)
	if c.BootstrapTokenHash != "" && !validHash.MatchString(c.BootstrapTokenHash) {
		return fmt.Errorf("invalid bootstrap credential hash")
	}
	ids := map[string]bool{}
	hashes := map[string]bool{}
	if c.BootstrapTokenHash != "" {
		hashes[c.BootstrapTokenHash] = true
	}
	for _, a := range c.Agents {
		if !identifier.MatchString(a.ID) || a.Name == "" || ids[a.ID] || !validHash.MatchString(a.TokenHash) || hashes[a.TokenHash] {
			return fmt.Errorf("invalid or duplicate agent credential")
		}
		ids[a.ID] = true
		hashes[a.TokenHash] = true
	}
	return c.validateReaders(validHash, ids, hashes)
}

func (c Config) validateReaders(validHash *regexp.Regexp, ids, hashes map[string]bool) error {
	subjects := map[string]bool{}
	for _, r := range c.Readers {
		if r.Subject == "" || subjects[r.Subject] || (r.Role != RoleAdmin && len(r.Clusters) == 0) {
			return fmt.Errorf("reader requires a unique subject and explicit cluster grants")
		}
		if r.Role != "" && r.Role != RoleAdmin && r.Role != RoleViewer {
			return fmt.Errorf("invalid reader role")
		}
		if r.Role == RoleAdmin && len(r.Namespaces) > 0 {
			return fmt.Errorf("administrators cannot have namespace restrictions")
		}
		subjects[r.Subject] = true
		if r.TokenHash != "" {
			if !validHash.MatchString(r.TokenHash) || hashes[r.TokenHash] {
				return fmt.Errorf("invalid or reused reader credential")
			}
			hashes[r.TokenHash] = true
		}
		for _, id := range r.Clusters {
			if !ids[id] {
				return fmt.Errorf("reader references unknown cluster %q", id)
			}
		}
	}
	return nil
}

func (c Config) validateAgentTLS() error {
	if c.AgentTLS != nil {
		u, e := url.Parse(c.AgentTLS.PublicURL)
		if e != nil || u.Scheme != schemeHTTPS || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || c.AgentTLS.Listen == "" {
			return fmt.Errorf("agentTLS requires an HTTPS public origin and explicit listener")
		}
	}
	return nil
}
