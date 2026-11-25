package v1alpha1

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
)

func TestRecommendationAnnotationKeysAcceptEveryContainerNameLength(t *testing.T) {
	fields := []string{"cpu-request", "memory-request", "cpu-limit", "memory-limit", "explanation", "raw-cpu-request", "raw-memory-request"}
	seen := map[string]string{}
	for length := 1; length <= 63; length++ {
		container := strings.Repeat("a", length)
		for _, field := range fields {
			key := RecommendationAnnotationKey(container, field)
			if errors := validation.IsQualifiedName(key); len(errors) != 0 {
				t.Fatalf("container length %d, field %s: invalid annotation %q: %v", length, field, key, errors)
			}
			identity := container + "/" + field
			if previous, ok := seen[key]; ok && previous != identity {
				t.Fatalf("%s and %s share annotation %s", previous, identity, key)
			}
			seen[key] = identity
			legacy := AnnotationRecommendationPrefix + "." + container + "." + field
			if len(validation.IsQualifiedName(legacy)) == 0 && key != legacy {
				t.Fatalf("valid legacy key changed: %s -> %s", legacy, key)
			}
		}
	}
}

func TestExistingHashedRecommendationKeyRemainsStable(t *testing.T) {
	got := RecommendationAnnotationKey(strings.Repeat("a", 63), "memory-request")
	const want = "optipod.io/recommendation._7d3e74a05d7db15bce4ad9ec0658ea98.memory-request"
	if got != want {
		t.Fatalf("existing hashed key changed: got %s, want %s", got, want)
	}
}
