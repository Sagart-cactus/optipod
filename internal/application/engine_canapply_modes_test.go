/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package application

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/version"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// TestCanApplyModeDecisions pins the production applicability decisions for
// each policy mode and the engine dry-run switch (VAL-FAST-002: Auto applies
// only when allowed). Recommend and Disabled policies never allow application,
// a global dry-run engine never applies, and an Auto policy with no usable
// update strategy is skipped with an explicit reason. The in-place/recreate
// preference cases are covered by the property tests in engine_test.go; this
// table adds the mode/dry-run dimension they never generate.
func TestCanApplyModeDecisions(t *testing.T) {
	newEngine := func(dryRun bool) *Engine {
		return &Engine{
			discoveryClient: &mockDiscoveryClient{
				serverVersion: &version.Info{Major: "1", Minor: "37"},
			},
			dryRun: dryRun,
		}
	}

	tests := []struct {
		name          string
		policyMode    string
		dryRun        bool
		allowInPlace  bool
		allowRecreate bool
		wantCanApply  bool
		wantMethod    ApplyMethod
		wantReason    string
	}{
		{
			name:         "Recommend mode never applies",
			policyMode:   "Recommend",
			allowInPlace: true,
			wantCanApply: false,
			wantMethod:   Skip,
			wantReason:   "Policy is in Recommend mode",
		},
		{
			name:         "Disabled mode never applies",
			policyMode:   "Disabled",
			allowInPlace: true,
			wantCanApply: false,
			wantMethod:   Skip,
			wantReason:   "Policy is disabled",
		},
		{
			name:         "a global dry-run engine never applies even in Auto mode",
			policyMode:   "Auto",
			dryRun:       true,
			allowInPlace: true,
			wantCanApply: false,
			wantMethod:   Skip,
			wantReason:   "Global dry-run mode is enabled",
		},
		{
			name:         "Auto mode with an allowed strategy applies in place",
			policyMode:   "Auto",
			allowInPlace: true,
			wantCanApply: true,
			wantMethod:   InPlace,
		},
		{
			name:          "Auto mode with no allowed strategy is skipped with a reason",
			policyMode:    "Auto",
			allowInPlace:  false,
			allowRecreate: false,
			wantCanApply:  false,
			wantMethod:    Skip,
			wantReason:    "No update strategy available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newEngine(tt.dryRun)
			policy := createMockPolicy(tt.allowInPlace, tt.allowRecreate)
			policy.Spec.Mode = optipodv1alpha1.PolicyMode(tt.policyMode)

			decision, err := engine.CanApply(
				context.Background(),
				createMockWorkload(),
				"test-container",
				createMockRecommendation(),
				policy,
			)
			if err != nil {
				t.Fatalf("CanApply returned an error: %v", err)
			}
			if decision.CanApply != tt.wantCanApply {
				t.Errorf("CanApply = %v, expected %v (reason %q)", decision.CanApply, tt.wantCanApply, decision.Reason)
			}
			if decision.Method != tt.wantMethod {
				t.Errorf("decision.Method = %q, expected %q", decision.Method, tt.wantMethod)
			}
			if tt.wantReason != "" && decision.Reason != tt.wantReason {
				t.Errorf("decision.Reason = %q, expected %q", decision.Reason, tt.wantReason)
			}
		})
	}
}
