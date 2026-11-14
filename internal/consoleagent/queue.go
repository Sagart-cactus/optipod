// Package consoleagent exports observed cluster state through a durable queue.
package consoleagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/optipod/optipod/internal/console"
	"golang.org/x/sys/unix"
)

var ErrQueueFull = errors.New("durable export queue is full; collection deferred")

type queueState struct {
	Binding  string            `json:"binding"`
	Stream   string            `json:"stream"`
	Sequence int64             `json:"sequence"`
	Seen     map[string]string `json:"seen"`
}

type Queue struct {
	Dir      string
	MaxBytes int64
	state    queueState
	lock     *os.File
}

func OpenQueue(dir, binding string, maxBytes int64) (*Queue, error) {
	if dir == "" || binding == "" || maxBytes < console.MaxBatchBytes {
		return nil, fmt.Errorf("queue requires directory, binding and at least 8MiB capacity")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("queue already has a writer")
	}
	q := &Queue{Dir: dir, MaxBytes: maxBytes, lock: lock, state: queueState{Binding: binding, Stream: uuid.NewString(), Seen: map[string]string{}}}
	if data, e := os.ReadFile(filepath.Join(dir, "state.json")); e == nil {
		if err = json.Unmarshal(data, &q.state); err != nil {
			q.Close()
			return nil, err
		}
	} else if !os.IsNotExist(e) {
		q.Close()
		return nil, e
	}
	if q.state.Binding != binding || q.state.Stream == "" {
		q.Close()
		return nil, fmt.Errorf("queue belongs to a different cluster")
	}
	if q.state.Seen == nil {
		q.state.Seen = map[string]string{}
	}
	// The lock excludes active writers, so abandoned atomic-write files are safe to remove.
	if err = q.cleanTemporaryFiles(); err != nil {
		q.Close()
		return nil, err
	}
	paths, err := q.Pending()
	if err != nil {
		q.Close()
		return nil, err
	}
	for _, path := range paths {
		b, e := readBatch(path)
		if e != nil {
			q.Close()
			return nil, e
		}
		if b.StreamID != q.state.Stream {
			q.Close()
			return nil, fmt.Errorf("queue stream is corrupt")
		}
		if b.Sequence > q.state.Sequence {
			q.state.Sequence = b.Sequence
		}
		q.remember(b)
	}
	if err = q.save(); err != nil {
		q.Close()
		return nil, err
	}
	return q, nil
}

func (q *Queue) cleanTemporaryFiles() error {
	paths, err := filepath.Glob(filepath.Join(q.Dir, ".pending-*"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return syncDirectory(q.Dir)
}

func (q *Queue) Close() {
	if q.lock != nil {
		_ = unix.Flock(int(q.lock.Fd()), unix.LOCK_UN)
		_ = q.lock.Close()
		q.lock = nil
	}
}

func (q *Queue) Pending() ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(q.Dir, "batch-*.json"))
	sort.Strings(paths)
	return paths, err
}

func (q *Queue) Enqueue(b *console.Batch) error {
	b.Version = console.ProtocolVersion
	b.StreamID = q.state.Stream
	b.Sequence = q.state.Sequence + 1
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if len(data) > console.MaxBatchBytes {
		return fmt.Errorf("one export batch exceeds 8MiB")
	}
	paths, err := q.Pending()
	if err != nil {
		return err
	}
	size := int64(len(data))
	for _, path := range paths {
		info, e := os.Stat(path)
		if e != nil {
			return e
		}
		size += info.Size()
	}
	if size > q.MaxBytes {
		return ErrQueueFull
	}
	path := filepath.Join(q.Dir, fmt.Sprintf("batch-%020d.json", b.Sequence))
	if err = atomicWrite(path, data); err != nil {
		return err
	}
	q.state.Sequence = b.Sequence
	q.remember(b)
	return q.save()
}

func (q *Queue) remember(b *console.Batch) {
	for _, r := range b.Records {
		q.state.Seen[r.Workload.Namespace+"/"+r.Name] = console.Digest(r)
	}
}

func (q *Queue) save() error {
	data, err := json.Marshal(q.state)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(q.Dir, "state.json"), data)
}

func (q *Queue) ForgetAbsent(present map[string]bool) error {
	for key := range q.state.Seen {
		if !present[key] {
			delete(q.state.Seen, key)
		}
	}
	return q.save()
}

func (q *Queue) Acknowledge(path string) error {
	if filepath.Dir(path) != q.Dir || !strings.HasPrefix(filepath.Base(path), "batch-") {
		return fmt.Errorf("invalid queue entry")
	}
	// Persist the sequence before removing its last recovery copy. A prior
	// state-file write may have failed after the batch itself was committed.
	if err := q.save(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(q.Dir)
}

func readBatch(path string) (*console.Batch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b console.Batch
	if err = json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}
