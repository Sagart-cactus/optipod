package consoleagent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/report"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Collector struct {
	Client     client.Client
	Queue      *Queue
	ClusterUID string
	Namespaces []string
}

// Capture queues complete inventory collections and new journal observations.
// An interrupted/failed collection never emits the deletion commit marker.
func (c *Collector) Capture(ctx context.Context) error {
	runID := uuid.NewString()
	at := time.Now().UTC()
	batch := func() *console.Batch { return &console.Batch{ClusterUID: c.ClusterUID, RunID: runID, CapturedAt: at} }
	current := batch()
	flush := func() error {
		if len(current.Workloads)+len(current.Policies)+len(current.Records) == 0 {
			return nil
		}
		if err := c.Queue.Enqueue(current); err != nil {
			return err
		}
		current = batch()
		return nil
	}
	room := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		existing, _ := json.Marshal(current)
		if len(data) > console.MaxBatchBytes/2 {
			return fmt.Errorf("one observed object exceeds 4MiB")
		}
		if len(existing)+len(data) > console.MaxBatchBytes/2 || len(current.Workloads)+len(current.Policies)+len(current.Records) >= 100 {
			return flush()
		}
		return nil
	}
	namespaces := c.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	present := map[string]bool{}
	warnings := []string{}
	for _, ns := range namespaces {
		fleet, err := (&report.Collector{Client: c.Client, Namespace: ns}).Fleet(ctx)
		if err != nil {
			return err
		}
		warnings = append(warnings, fleet.Warnings...)
		for _, w := range fleet.Workloads {
			if err = room(w); err != nil {
				return err
			}
			current.Workloads = append(current.Workloads, w)
		}
		policies := &api.OptimizationPolicyList{}
		if err = c.Client.List(ctx, policies, client.InNamespace(ns)); err != nil {
			return err
		}
		for _, p := range policies.Items {
			policy := console.Policy{UID: string(p.UID), Namespace: p.Namespace, Name: p.Name, Generation: p.Generation, Spec: p.Spec}
			if err = room(policy); err != nil {
				return err
			}
			current.Policies = append(current.Policies, policy)
		}
		cms := &corev1.ConfigMapList{}
		if err = c.Client.List(ctx, cms, client.InNamespace(ns), client.MatchingLabels{history.Label: "true"}); err != nil {
			return err
		}
		for i := range cms.Items {
			r, e := history.FromConfigMap(&cms.Items[i])
			if e != nil {
				return fmt.Errorf("invalid journal %s/%s", cms.Items[i].Namespace, cms.Items[i].Name)
			}
			key := r.Workload.Namespace + "/" + r.Name
			present[key] = true
			if c.Queue.state.Seen[key] == console.Digest(r) {
				continue
			}
			if err = room(r); err != nil {
				return err
			}
			current.Records = append(current.Records, r)
		}
	}
	if err := flush(); err != nil {
		return err
	}
	current.Complete = true
	current.Warnings = warnings
	if err := c.Queue.Enqueue(current); err != nil {
		return err
	}
	return c.Queue.ForgetAbsent(present)
}
