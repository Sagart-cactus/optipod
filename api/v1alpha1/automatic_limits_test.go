package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAutomaticMemoryLimit(t *testing.T) {
	for _, test := range []struct{ request, current, want string }{
		{"32Mi", "0", "128Mi"}, {"120Mi", "64Mi", "180Mi"},
		{"120Mi", "1Gi", "1Gi"}, {"134217729", "0", "201326594"},
	} {
		t.Run(test.request+"/"+test.current, func(t *testing.T) {
			current := resource.MustParse(test.current)
			got := AutomaticMemoryLimit(resource.MustParse(test.request), &current)
			if got.Cmp(resource.MustParse(test.want)) != 0 {
				t.Fatalf("got %s, want %s", got.String(), test.want)
			}
		})
	}
}
