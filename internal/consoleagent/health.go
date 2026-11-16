package consoleagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/optipod/optipod/internal/console"
)

func (q *Queue) Bytes() (int64, error) {
	paths, err := q.Pending()
	if err != nil {
		return 0, err
	}
	var size int64
	for _, path := range paths {
		info, e := os.Stat(path)
		if e != nil {
			return 0, e
		}
		size += info.Size()
	}
	return size, nil
}

func (u *Uploader) Heartbeat(ctx context.Context, health console.AgentHealth) error {
	if err := u.validateURL(); err != nil {
		return err
	}
	token, err := os.ReadFile(u.TokenFile)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(health)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(u.URL, "/")+"/api/heartbeat", bytes.NewReader(data))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	c := u.Client
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := c.Do(r)
	if err != nil {
		return fmt.Errorf("heartbeat unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat rejected (HTTP %d)", response.StatusCode)
	}
	return nil
}
