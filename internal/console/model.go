// Package console implements the persistent, read-only multi-cluster console.
package console

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/report"
)

const ProtocolVersion = 1

const resourcePolicies = "policies"
const resourceWorkloads = "workloads"

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// Batch is an ordered delivery from one persistent agent stream. Inventory
// deletion is committed only by the final batch of a successful collection.
type Batch struct {
	Version    int               `json:"version"`
	ClusterUID string            `json:"clusterUID"`
	StreamID   string            `json:"streamID"`
	Sequence   int64             `json:"sequence"`
	CapturedAt time.Time         `json:"capturedAt"`
	RunID      string            `json:"runID"`
	Complete   bool              `json:"complete"`
	Workloads  []report.Workload `json:"workloads,omitempty"`
	Policies   []Policy          `json:"policies,omitempty"`
	Records    []*history.Record `json:"records,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
}

type Policy struct {
	UID        string                     `json:"uid"`
	Namespace  string                     `json:"namespace"`
	Name       string                     `json:"name"`
	Generation int64                      `json:"generation"`
	Spec       api.OptimizationPolicySpec `json:"spec"`
}

func Digest(value any) string {
	b, _ := json.Marshal(value)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func (b *Batch) Validate() error {
	if b.Version != ProtocolVersion || !identifier.MatchString(b.ClusterUID) || !identifier.MatchString(b.StreamID) || !identifier.MatchString(b.RunID) || b.Sequence < 1 || b.CapturedAt.IsZero() || b.CapturedAt.After(time.Now().Add(5*time.Minute)) {
		return fmt.Errorf("invalid batch identity, version or timestamp")
	}
	if len(b.Workloads)+len(b.Policies)+len(b.Records) > 100 {
		return fmt.Errorf("batch exceeds 100 objects")
	}
	for _, w := range b.Workloads {
		if w.UID == "" || w.Namespace == "" || w.Name == "" || w.Kind == "" {
			return fmt.Errorf("invalid workload identity")
		}
	}
	for _, p := range b.Policies {
		if p.UID == "" || p.Namespace == "" || p.Name == "" || p.Generation < 1 {
			return fmt.Errorf("invalid policy identity")
		}
	}
	for _, r := range b.Records {
		if r == nil || r.Version != 1 || r.Name == "" || r.Workload.UID == "" || r.Workload.Namespace == "" || r.Created.IsZero() {
			return fmt.Errorf("invalid history record")
		}
	}
	return nil
}

type Cluster struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	UID          string     `json:"uid"`
	LastSequence int64      `json:"lastSequence"`
	CapturedAt   *time.Time `json:"capturedAt"`
	ReceivedAt   *time.Time `json:"receivedAt"`
	Warnings     []string   `json:"warnings"`
}

type Query struct {
	Namespaces                                    []string
	Clusters                                      []string
	Cluster, Namespace, UID, Search, Mode, Cursor string
	Limit                                         int
	IncludeDeleted                                bool
}

type Item struct {
	ID         int64           `json:"id"`
	ClusterID  string          `json:"clusterID"`
	ObservedAt time.Time       `json:"observedAt"`
	Deleted    bool            `json:"deleted"`
	Data       json.RawMessage `json:"data"`
}

type Page struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}
