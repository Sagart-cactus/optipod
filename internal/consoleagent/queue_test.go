package consoleagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/optipod/optipod/internal/console"
)

func queueBatch() *console.Batch {
	return &console.Batch{ClusterUID: "cluster", RunID: "run", CapturedAt: time.Now().UTC(), Complete: true}
}

func TestDurableQueueReplayAndAcknowledgment(t *testing.T) {
	dir := t.TempDir()
	q, err := OpenQueue(dir, "binding", 16*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = OpenQueue(dir, "binding", 16*1024*1024); err == nil {
		t.Fatal("second writer acquired queue")
	}
	if err = q.Enqueue(queueBatch()); err != nil {
		t.Fatal(err)
	}
	firstStream := q.state.Stream
	q.Close()
	q, err = OpenQueue(dir, "binding", 16*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if q.state.Stream != firstStream || q.state.Sequence != 1 {
		t.Fatal("stream lost on restart")
	}
	if err = q.Enqueue(queueBatch()); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err = os.WriteFile(tokenFile, []byte("credential"), 0600); err != nil {
		t.Fatal(err)
	}
	mode := "outage"
	sequences := []int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer credential" {
			t.Error("missing scoped credential")
		}
		if mode == "outage" {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		var b console.Batch
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		sequences = append(sequences, b.Sequence)
		ack := b.Sequence
		if mode == "wrong-ack" {
			ack++
		}
		_ = json.NewEncoder(w).Encode(map[string]int64{"acknowledged": ack})
	}))
	defer server.Close()
	u := Uploader{URL: server.URL, TokenFile: tokenFile, AllowHTTP: true}
	if err = u.Drain(context.Background(), q); err == nil {
		t.Fatal("outage acknowledged")
	}
	paths, _ := q.Pending()
	if len(paths) != 2 {
		t.Fatal("outage lost queued events")
	}
	mode = "wrong-ack"
	if err = u.Drain(context.Background(), q); err == nil {
		t.Fatal("wrong ACK accepted")
	}
	paths, _ = q.Pending()
	if len(paths) != 2 {
		t.Fatal("wrong ACK removed data")
	}
	mode = "success"
	sequences = nil
	if err = u.Drain(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	paths, _ = q.Pending()
	if len(paths) != 0 || len(sequences) != 2 || sequences[0] != 1 || sequences[1] != 2 {
		t.Fatalf("ordered replay: %v %v", paths, sequences)
	}
	q.Close()
	q, err = OpenQueue(dir, "binding", 16*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if err = q.Enqueue(queueBatch()); err != nil {
		t.Fatal(err)
	}
	if q.state.Sequence != 3 {
		t.Fatal("sequence reset after drain/restart")
	}
}

func TestQueueCapacityAndBinding(t *testing.T) {
	dir := t.TempDir()
	q, err := OpenQueue(dir, "one", console.MaxBatchBytes)
	if err != nil {
		t.Fatal(err)
	}
	b := queueBatch()
	b.Warnings = []string{strings.Repeat("x", 5*1024*1024)}
	if err = q.Enqueue(b); err != nil {
		t.Fatal(err)
	}
	if err = q.Enqueue(b); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected bounded queue: %v", err)
	}
	if q.state.Sequence != 1 {
		t.Fatal("failed capture consumed sequence")
	}
	q.Close()
	if _, err = OpenQueue(dir, "two", console.MaxBatchBytes); err == nil {
		t.Fatal("different cluster reused queue")
	}
}

func TestAcknowledgmentRequiresDurableState(t *testing.T) {
	dir := t.TempDir()
	q, err := OpenQueue(dir, "binding", console.MaxBatchBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if err = q.Enqueue(queueBatch()); err != nil {
		t.Fatal(err)
	}
	paths, _ := q.Pending()
	// Simulate a state-file failure after the batch has been written.
	if err = os.Remove(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(dir, "state.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = q.Acknowledge(paths[0]); err == nil {
		t.Fatal("removed last recovery copy without durable state")
	}
	if _, err = os.Stat(paths[0]); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err = q.Acknowledge(paths[0]); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, ".pending-orphan"), []byte("unfinished"), 0600); err != nil {
		t.Fatal(err)
	}
	q.Close()
	q, err = OpenQueue(dir, "binding", console.MaxBatchBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if q.state.Sequence != 1 {
		t.Fatal("acknowledged sequence lost")
	}
	if _, err = os.Stat(filepath.Join(dir, ".pending-orphan")); !os.IsNotExist(err) {
		t.Fatal("abandoned write retained")
	}
}
