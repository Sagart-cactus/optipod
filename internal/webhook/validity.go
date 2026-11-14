package webhook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InvalidateRecommendations revokes only the plan produced by this policy.
// ResourceVersion makes concurrent replacement by another policy a conflict.
func InvalidateRecommendations(ctx context.Context, c client.Client, workload client.Object, p *api.OptimizationPolicy) error {
	current := workload.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(workload), current); err != nil {
		return err
	}
	a := current.GetAnnotations()
	if a[AppliedPolicyUID] != string(p.UID) || a[RecommendationExpiry] == "" {
		return nil
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": workload.GetUID(), "resourceVersion": current.GetResourceVersion(), "annotations": map[string]any{RecommendationExpiry: nil}}})
	return c.Patch(ctx, current, client.RawPatch(types.MergePatchType, patch))
}

const (
	AppliedPolicyUID        = "optipod.io/applied-policy-uid"
	AppliedPolicyGeneration = "optipod.io/applied-policy-generation"
	RecommendationExpiry    = "optipod.io/recommendation-expires-at"
	RecommendationHash      = "optipod.io/recommendation-hash"
	RolloutHash             = "optipod.io/rollout-hash"
)

// StampRecommendations binds a complete resource plan to the policy revision
// that produced it. Display-only recommendation annotations cannot renew it.
func StampRecommendations(a map[string]string, p *api.OptimizationPolicy, now time.Time) {
	ttl := p.Spec.UpdateStrategy.RecommendationTTL.Duration
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	a[AppliedPolicyUID] = string(p.UID)
	a[AppliedPolicyGeneration] = strconv.FormatInt(p.Generation, 10)
	a[RecommendationExpiry] = now.Add(ttl).UTC().Format(time.RFC3339Nano)
}

func ValidRecommendations(a map[string]string, p *api.OptimizationPolicy, now time.Time) bool {
	if a[AppliedPolicyUID] != string(p.UID) || a[AppliedPolicyGeneration] != strconv.FormatInt(p.Generation, 10) {
		return false
	}
	expires, err := time.Parse(time.RFC3339Nano, a[RecommendationExpiry])
	return err == nil && now.Before(expires)
}

func resourcePlanHash(recs []ResourceRecommendation) string {
	copyRecs := append([]ResourceRecommendation(nil), recs...)
	sort.Slice(copyRecs, func(i, j int) bool { return copyRecs[i].ContainerName < copyRecs[j].ContainerName })
	data, _ := json.Marshal(copyRecs)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
