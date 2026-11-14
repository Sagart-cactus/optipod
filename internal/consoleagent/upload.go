package consoleagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Uploader struct {
	URL       string
	TokenFile string
	Client    *http.Client
	AllowHTTP bool
}

func (u *Uploader) Drain(ctx context.Context, q *Queue) error {
	endpoint, err := url.Parse(u.URL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "https" && (endpoint.Scheme != "http" || !u.AllowHTTP)) {
		return fmt.Errorf("upload URL requires HTTPS (explicit HTTP opt-in for local tests)")
	}
	secret, err := os.ReadFile(u.TokenFile)
	if err != nil {
		return err
	}
	paths, err := q.Pending()
	if err != nil {
		return err
	}
	httpClient := u.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	for _, path := range paths {
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		batch, e := readBatch(path)
		if e != nil {
			return e
		}
		r, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(u.URL, "/")+"/api/ingest", bytes.NewReader(data))
		if e != nil {
			return e
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(secret)))
		response, e := httpClient.Do(r)
		if e != nil {
			return fmt.Errorf("upload unavailable: %w", e)
		}
		var ack struct {
			Sequence int64 `json:"acknowledged"`
		}
		e = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&ack)
		_ = response.Body.Close()
		if response.StatusCode != 200 || e != nil || ack.Sequence != batch.Sequence {
			return fmt.Errorf("upload not acknowledged (HTTP %d); queued data retained", response.StatusCode)
		}
		if e = q.Acknowledge(path); e != nil {
			return e
		}
	}
	return nil
}
