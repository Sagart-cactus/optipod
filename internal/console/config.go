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

type AgentCredential struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TokenHash string `json:"tokenHash"`
}

type Reader struct {
	Subject    string   `json:"subject"`
	TokenHash  string   `json:"tokenHash,omitempty"`
	Clusters   []string `json:"clusters"`
	Namespaces []string `json:"namespaces,omitempty"`
}

type OIDCConfig struct {
	Issuer           string `json:"issuer"`
	ClientID         string `json:"clientID"`
	ClientSecretFile string `json:"clientSecretFile,omitempty"`
}

type Config struct {
	PublicURL string            `json:"publicURL"`
	Agents    []AgentCredential `json:"agents"`
	Readers   []Reader          `json:"readers"`
	OIDC      *OIDCConfig       `json:"oidc,omitempty"`
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
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("publicURL must be an absolute origin")
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !loopback) {
		return fmt.Errorf("publicURL requires HTTPS except on loopback")
	}
	validHash := regexp.MustCompile(`^[a-f0-9]{64}$`)
	ids := map[string]bool{}
	hashes := map[string]bool{}
	subjects := map[string]bool{}
	for _, a := range c.Agents {
		if !identifier.MatchString(a.ID) || a.Name == "" || ids[a.ID] || !validHash.MatchString(a.TokenHash) || hashes[a.TokenHash] {
			return fmt.Errorf("invalid or duplicate agent credential")
		}
		ids[a.ID] = true
		hashes[a.TokenHash] = true
	}
	for _, r := range c.Readers {
		if r.Subject == "" || subjects[r.Subject] || len(r.Clusters) == 0 {
			return fmt.Errorf("reader requires a unique subject and explicit cluster grants")
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
	if len(c.Agents) == 0 || len(c.Readers) == 0 {
		return fmt.Errorf("configure at least one agent and reader")
	}
	return nil
}
