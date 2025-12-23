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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestGetCurrentResourcesWithInvalidQuantities tests that invalid resource quantities
// return errors instead of panicking the controller
func TestGetCurrentResourcesWithInvalidQuantities(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name        string
		workload    *Workload
		expectError bool
		errorMsg    string
	}{
		{
			name: "invalid CPU request",
			workload: &Workload{
				Kind:      "Deployment",
				Namespace: "test",
				Name:      "test-deployment",
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"containers": []interface{}{
										map[string]interface{}{
											"name": "test-container",
											"resources": map[string]interface{}{
												"requests": map[string]interface{}{
													"cpu":    "invalid-cpu-value",
													"memory": "128Mi",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid CPU request quantity",
		},
		{
			name: "invalid memory request",
			workload: &Workload{
				Kind:      "Deployment",
				Namespace: "test",
				Name:      "test-deployment",
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"containers": []interface{}{
										map[string]interface{}{
											"name": "test-container",
											"resources": map[string]interface{}{
												"requests": map[string]interface{}{
													"cpu":    "100m",
													"memory": "not-a-memory-value",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid memory request quantity",
		},
		{
			name: "invalid CPU limit",
			workload: &Workload{
				Kind:      "Deployment",
				Namespace: "test",
				Name:      "test-deployment",
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"containers": []interface{}{
										map[string]interface{}{
											"name": "test-container",
											"resources": map[string]interface{}{
												"limits": map[string]interface{}{
													"cpu":    "invalid-cpu-limit",
													"memory": "256Mi",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid CPU limit quantity",
		},
		{
			name: "invalid memory limit",
			workload: &Workload{
				Kind:      "Deployment",
				Namespace: "test",
				Name:      "test-deployment",
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"containers": []interface{}{
										map[string]interface{}{
											"name": "test-container",
											"resources": map[string]interface{}{
												"limits": map[string]interface{}{
													"cpu":    "200m",
													"memory": "invalid-memory-limit",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid memory limit quantity",
		},
		{
			name: "valid quantities should work",
			workload: &Workload{
				Kind:      "Deployment",
				Namespace: "test",
				Name:      "test-deployment",
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"containers": []interface{}{
										map[string]interface{}{
											"name": "test-container",
											"resources": map[string]interface{}{
												"requests": map[string]interface{}{
													"cpu":    "100m",
													"memory": "128Mi",
												},
												"limits": map[string]interface{}{
													"cpu":    "200m",
													"memory": "256Mi",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "empty resource values should be skipped",
			workload: &Workload{
				Kind:      "Deployment",
				Namespace: "test",
				Name:      "test-deployment",
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"containers": []interface{}{
										map[string]interface{}{
											"name": "test-container",
											"resources": map[string]interface{}{
												"requests": map[string]interface{}{
													// Empty requests should be handled gracefully
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This should not panic, even with invalid input
			resources, err := engine.getCurrentResources(tt.workload)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, but got no error", tt.errorMsg)
					return
				}
				if err.Error() == "" {
					t.Errorf("Expected non-empty error message")
					return
				}
				// Check that error message contains expected text
				if tt.errorMsg != "" {
					if len(err.Error()) == 0 || err.Error()[:len(tt.errorMsg)] != tt.errorMsg {
						t.Errorf("Expected error message to start with %q, got %q", tt.errorMsg, err.Error())
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
					return
				}
				if resources == nil {
					t.Errorf("Expected resources map, but got nil")
					return
				}
			}
		})
	}
}

// TestGetCurrentResourcesNoPanic ensures that the function never panics,
// even with completely malformed input
func TestGetCurrentResourcesNoPanic(t *testing.T) {
	engine := &Engine{}

	// Test with completely malformed workload that could cause panics
	malformedWorkload := &Workload{
		Kind:      "Deployment",
		Namespace: "test",
		Name:      "malformed",
		Object: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name": "test-container",
									"resources": map[string]interface{}{
										"requests": map[string]interface{}{
											"cpu":    "definitely-not-a-cpu-value",
											"memory": "this-is-not-memory",
										},
										"limits": map[string]interface{}{
											"cpu":    "also-invalid",
											"memory": "still-invalid",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// This test ensures we don't panic - we should get an error instead
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("getCurrentResources panicked with: %v", r)
		}
	}()

	_, err := engine.getCurrentResources(malformedWorkload)
	if err == nil {
		t.Error("Expected error for malformed resource quantities, but got none")
	}
}