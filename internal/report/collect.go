package report

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/coexist"
	"github.com/optipod/optipod/internal/discovery"
	"github.com/optipod/optipod/internal/history"
	"github.com/optipod/optipod/internal/nodesizing"
	"github.com/optipod/optipod/internal/policy"
	"github.com/optipod/optipod/internal/webhook"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	resourcehelper "k8s.io/component-helpers/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Collector struct {
	Client    client.Client
	Namespace string
}

func (c *Collector) Fleet(ctx context.Context) (*Fleet, error) {
	result := &Fleet{Generated: time.Now().UTC(), Workloads: []Workload{}}
	policies := &api.OptimizationPolicyList{}
	if err := c.Client.List(ctx, policies); err != nil {
		return nil, err
	}
	result.Policies = len(policies.Items)
	seen := map[types.UID]bool{}
	workloads := []discovery.Workload{}
	for i := range policies.Items {
		found, err := discovery.DiscoverWorkloads(ctx, c.Client, &policies.Items[i])
		if err != nil {
			return nil, err
		}
		for _, w := range found {
			if c.Namespace != "" && w.Namespace != c.Namespace {
				continue
			}
			if !seen[w.Object.GetUID()] {
				workloads = append(workloads, w)
				seen[w.Object.GetUID()] = true
			}
		}
	}
	podsByOwner := map[types.UID][]corev1.Pod{}
	pods := &corev1.PodList{}
	if err := c.Client.List(ctx, pods, client.InNamespace(c.Namespace)); err != nil {
		return nil, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		owner, err := discovery.ResolvePodWorkload(ctx, c.Client, pod)
		if err != nil {
			continue
		}
		if seen[owner.Object.GetUID()] {
			podsByOwner[owner.Object.GetUID()] = append(podsByOwner[owner.Object.GetUID()], *pod)
		}
	}
	events := &corev1.EventList{}
	if err := c.Client.List(ctx, events, client.InNamespace(c.Namespace)); err != nil {
		result.Warnings = append(result.Warnings, "Events unavailable: "+err.Error())
	}
	for _, w := range workloads {
		p, err := policy.NewPolicySelector(c.Client).SelectBestPolicy(ctx, &w)
		if err != nil {
			return nil, err
		}
		if p == nil {
			continue
		}
		a := w.Object.GetAnnotations()
		row := Workload{Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, UID: string(w.Object.GetUID()), Policy: p.Namespace + "/" + p.Name, Mode: p.Spec.Mode, Provider: p.Spec.MetricsConfig.Provider, Paused: a[history.Paused] == "true", RecommendationExpires: a[webhook.RecommendationExpiry], LatestRecord: a[history.Latest], Pods: []Pod{}, Containers: []Container{}}
		if p.Spec.DaemonSetSizing != nil {
			row.Provider = "node allocatable (admission)"
			if raw := a[nodesizing.SummaryAnnotation]; raw != "" {
				if err := json.Unmarshal([]byte(raw), &row.NodePlans); err != nil {
					result.Warnings = append(result.Warnings, "Invalid node preview for "+w.Namespace+"/"+w.Name)
				}
			}
		}
		t, err := discovery.Template(w.Object)
		if err != nil {
			return nil, err
		}
		populateContainers(&row, &t.Spec, a, p.Spec.Mode)
		populatePods(&row, podsByOwner[w.Object.GetUID()])
		restrictions, err := coexist.Read(ctx, c.Client, w.Namespace, w.Kind, w.Name)
		if err != nil {
			return nil, err
		}
		row.Scalers = restrictions.Targets
		latest := time.Time{}
		for _, event := range events.Items {
			if event.InvolvedObject.UID != p.UID || !strings.Contains(event.Message, w.Namespace+"/"+w.Name) {
				continue
			}
			stamp := event.LastTimestamp.Time
			if event.EventTime.After(stamp) {
				stamp = event.EventTime.Time
			}
			if event.Series != nil && event.Series.LastObservedTime.After(stamp) {
				stamp = event.Series.LastObservedTime.Time
			}
			if stamp.After(latest) {
				latest = stamp
				row.Reason = event.Message
			}
		}
		sort.Slice(row.Pods, func(i, j int) bool { return row.Pods[i].Name < row.Pods[j].Name })
		result.Workloads = append(result.Workloads, row)
	}
	sort.Slice(result.Workloads, func(i, j int) bool {
		a, b := result.Workloads[i], result.Workloads[j]
		return a.Namespace+"/"+a.Kind+"/"+a.Name < b.Namespace+"/"+b.Kind+"/"+b.Name
	})
	return result, nil
}
func (c *Collector) History(ctx context.Context, namespace, uid string) (*Journal, error) {
	if c.Namespace != "" && namespace != c.Namespace {
		return nil, fmt.Errorf("namespace is outside the selected report scope")
	}
	cms := &corev1.ConfigMapList{}
	labels := client.MatchingLabels{history.Label: "true"}
	if uid != "" {
		labels[history.WorkloadLabel] = uid
	}
	if err := c.Client.List(ctx, cms, client.InNamespace(namespace), labels); err != nil {
		return nil, err
	}
	result := &Journal{Generated: time.Now().UTC(), Records: []*history.Record{}}
	for _, cm := range cms.Items {
		record, err := history.Read(ctx, c.Client, cm.Namespace, cm.Name)
		if err != nil {
			return nil, err
		}
		result.Records = append(result.Records, record)
	}
	sort.Slice(result.Records, func(i, j int) bool { return result.Records[i].Created.After(result.Records[j].Created) })
	return result, nil
}

func populatePods(row *Workload, pods []corev1.Pod) {
	for _, pod := range pods {
		pv := Pod{Name: pod.Name, UID: string(pod.UID), Node: pod.Spec.NodeName, Containers: history.Resources(&pod.Spec)}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				pv.Ready = true
			}
		}
		for _, status := range append(pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses...) {
			pv.Restarts += status.RestartCount
		}
		row.Pods = append(row.Pods, pv)
		if pod.DeletionTimestamp == nil && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			resources := resourcehelper.PodRequests(&pod, resourcehelper.PodResourcesOptions{})
			row.RequestedMilliCPU += resources.Cpu().MilliValue()
			row.RequestedMemoryBytes += resources.Memory().Value()
		}
	}
}

func populateContainers(row *Workload, spec *corev1.PodSpec, a map[string]string, mode api.PolicyMode) {
	for _, slot := range discovery.ContainerSlots(spec) {
		co := Container{Name: slot.Container.Name, Current: slot.Container.Resources, Recommended: map[string]string{}}
		for _, field := range []string{"cpu-request", "memory-request", "cpu-limit", "memory-limit", "explanation"} {
			value := a["optipod.io/recommendation."+co.Name+"."+field]
			if mode == api.ModeAuto {
				if v := a["optipod.io/"+field+"."+co.Name]; v != "" {
					value = v
				}
			}
			if value != "" {
				co.Recommended[field] = value
			}
		}
		row.Containers = append(row.Containers, co)
	}
}
