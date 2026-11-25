package v1alpha1

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// RecommendationAnnotationKey preserves existing keys where Kubernetes permits
// them, and uses a stable container digest when the name would exceed 63 bytes.
// The underscore cannot occur in a container name, so digest keys cannot collide
// with the legacy spelling of another valid container name.
func RecommendationAnnotationKey(container, field string) string {
	key := AnnotationRecommendationPrefix + "." + container + "." + field
	if len(key[strings.IndexByte(key, '/')+1:]) <= 63 {
		return key
	}
	digest := sha256.Sum256([]byte(container))
	prefix := AnnotationRecommendationPrefix[strings.IndexByte(AnnotationRecommendationPrefix, '/')+1:]
	// Longer fields need a shorter digest to fit; existing keys retain 16 bytes.
	digestBytes := max(1, min(16, (63-len(prefix)-len(field)-3)/2))
	return fmt.Sprintf("%s._%x.%s", AnnotationRecommendationPrefix, digest[:digestBytes], field)
}
