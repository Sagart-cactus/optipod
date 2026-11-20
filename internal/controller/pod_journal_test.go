package controller

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/types"
)

func TestPodJournalRetainsIdentitiesWithinAnnotationBudget(t *testing.T) {
	start := time.Now().UTC()
	pods := make([]observedPod, 1350)
	for i := range pods {
		uid := types.UID(fmt.Sprintf("pod-%d", i))
		pods[i] = observedPod{Name: string(uid), UID: uid, Created: start, Started: map[string]time.Time{"worker": start}, Containers: []string{"worker"}}
	}
	for _, group := range [][]observedPod{pods[:2], pods} {
		value, err := encodePodJournal(group, nil)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(value, compressedJournalPrefix) != (len(group) > 2) {
			t.Fatal("unexpected journal encoding")
		}
		decoded, err := decodePodJournal(value)
		if err != nil || len(decoded) != len(group) {
			t.Fatalf("decoded %d pods: %v", len(decoded), err)
		}
		for i, p := range decoded {
			if p.UID != group[i].UID || !p.Started["worker"].Equal(start) {
				t.Fatalf("lost identity or start for pod %d", i)
			}
		}
	}
	if _, err := encodePodJournal(pods, map[string]string{"other": strings.Repeat("a", validation.TotalAnnotationSizeLimitB-100)}); err == nil {
		t.Fatal("expected an annotation size error")
	}
	if _, err := decodePodJournal(compressedJournalPrefix + "broken"); err == nil {
		t.Fatal("expected an invalid archive error")
	}
}
