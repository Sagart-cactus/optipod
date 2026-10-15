package suite

import (
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestManagedFieldLeaves pins the FieldsV1 decoding the SSA and webhook
// ownership assertions rely on: "f:" segments, associative "k:{...}" list keys
// and positional "i:" keys normalize to stable leaf paths, and malformed or
// missing sets fail loudly instead of silently passing ownership checks.
func TestManagedFieldLeaves(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{
			name: "deployment SSA set with two applied containers",
			raw: `{"f:metadata":{"f:name":{},"f:namespace":{}},` +
				`"f:spec":{"f:template":{"f:spec":{"f:containers":{` +
				`"k:{\"name\":\"app\"}":{"f:resources":{"f:requests":{"f:cpu":{},"f:memory":{}}}},` +
				`"k:{\"name\":\"sidecar\"}":{"f:resources":{"f:requests":{"f:cpu":{},"f:memory":{}}}}}}}}}`,
			want: []string{
				"metadata/name",
				"metadata/namespace",
				"spec/template/spec/containers[name=app]/resources/requests/cpu",
				"spec/template/spec/containers[name=app]/resources/requests/memory",
				"spec/template/spec/containers[name=sidecar]/resources/requests/cpu",
				"spec/template/spec/containers[name=sidecar]/resources/requests/memory",
			},
		},
		{
			name: "pod set with labels, annotations and container merge keys",
			raw: `{"f:metadata":{"f:annotations":{"f:team.example.com~1owner":{}},"f:labels":{"f:app":{}}},` +
				`"f:spec":{"f:containers":{"k:{\"name\":\"app\"}":{"f:image":{},"f:resources":{"f:requests":{"f:cpu":{},"f:memory":{}}}}}}}`,
			want: []string{
				"metadata/annotations/team.example.com~1owner",
				"metadata/labels/app",
				"spec/containers[name=app]/image",
				"spec/containers[name=app]/resources/requests/cpu",
				"spec/containers[name=app]/resources/requests/memory",
			},
		},
		{
			name:    "invalid JSON fails",
			raw:     `{"f:spec":`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := metav1.ManagedFieldsEntry{
				Manager:   "optipod",
				Operation: metav1.ManagedFieldsOperationApply,
				FieldsV1:  metav1.NewFieldsV1(tt.raw),
			}
			got, err := managedFieldLeaves(entry)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected a decode error, got leaves %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("managedFieldLeaves: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("leaves mismatch:\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}

	t.Run("missing FieldsV1 set fails", func(t *testing.T) {
		if _, err := managedFieldLeaves(metav1.ManagedFieldsEntry{Manager: "optipod"}); err == nil {
			t.Fatal("expected an error for an entry without a FieldsV1 set")
		}
	})

	t.Run("positional keys normalize", func(t *testing.T) {
		entry := metav1.ManagedFieldsEntry{
			Manager:   "optipod",
			Operation: metav1.ManagedFieldsOperationUpdate,
			FieldsV1:  metav1.NewFieldsV1(`{"f:spec":{"f:volumes":{"i:0":{"f:name":{}}}}}`),
		}
		got, err := managedFieldLeaves(entry)
		if err != nil {
			t.Fatalf("managedFieldLeaves: %v", err)
		}
		want := []string{"spec/volumes[0]/name"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("leaves mismatch: got %q want %q", got, want)
		}
	})
}

// TestUnrelatedLeafFragments pins that the fragments the SSA ownership
// assertion treats as forbidden cannot match any leaf the production apply is
// expected to own, but do match the unrelated leaves it must never own.
func TestUnrelatedLeafFragments(t *testing.T) {
	for _, leaf := range []string{
		"spec/template/spec/containers[name=app]/resources/requests/cpu",
		"spec/template/spec/containers[name=sidecar]/resources/requests/memory",
		"metadata/name",
		"spec/template/spec/containers[name=app]/name",
	} {
		for _, fragment := range unrelatedLeafFragments {
			if strings.Contains(leaf, fragment) {
				t.Fatalf("fragment %q wrongly matches the intended leaf %q", fragment, leaf)
			}
		}
	}
	for _, leaf := range []string{
		"metadata/annotations/team.example.com~1owner",
		"metadata/labels/team.example.com~1tier",
		"spec/template/metadata/labels/app",
		"spec/replicas",
		"spec/template/spec/containers[name=app]/image",
		"spec/template/spec/containers[name=app]/env/0/name",
		"spec/template/spec/terminationGracePeriodSeconds",
		"spec/template/spec/containers[name=app]/resources/limits/cpu",
	} {
		matched := false
		for _, fragment := range unrelatedLeafFragments {
			if strings.Contains(leaf, fragment) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("no forbidden fragment matches the unrelated leaf %q", leaf)
		}
	}
}
