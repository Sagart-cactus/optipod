package fixtures

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// TrueString represents the string "true" used in labels and comparisons
	TrueString = "true"
)

// PolicyConfigGenerator generates OptimizationPolicy configurations for testing
type PolicyConfigGenerator struct {
	rand *rand.Rand
}

// NewPolicyConfigGenerator creates a new PolicyConfigGenerator
func NewPolicyConfigGenerator() *PolicyConfigGenerator {
	return &PolicyConfigGenerator{
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// GenerateBasicPolicyConfig generates a basic policy configuration
func (g *PolicyConfigGenerator) GenerateBasicPolicyConfig(name string, mode v1alpha1.PolicyMode) helpers.PolicyConfig {
	return helpers.PolicyConfig{
		Name: name,
		Mode: mode,
		NamespaceSelector: map[string]string{
			"environment": "test",
		},
		WorkloadSelector: map[string]string{
			"optimize": "true",
		},
		ResourceBounds: helpers.ResourceBounds{
			CPU: helpers.ResourceBound{
				Min: "100m",
				Max: "2000m",
			},
			Memory: helpers.ResourceBound{
				Min: "128Mi",
				Max: "2Gi",
			},
		},
		MetricsConfig: helpers.MetricsConfig{
			Provider:      "metrics-server",
			RollingWindow: "1h",
			Percentile:    "P90",
			SafetyFactor:  1.2,
		},
		UpdateStrategy: helpers.UpdateStrategy{
			AllowInPlaceResize: true,
			AllowRecreate:      false,
			UpdateRequestsOnly: true,
		},
		ReconciliationInterval: &metav1.Duration{Duration: 1 * time.Minute},
	}
}

// GeneratePolicyWithBounds generates a policy with specific resource bounds
func (g *PolicyConfigGenerator) GeneratePolicyWithBounds(
	name string,
	cpuMin, cpuMax, memMin, memMax string,
) helpers.PolicyConfig {
	config := g.GenerateBasicPolicyConfig(name, v1alpha1.ModeRecommend)
	config.ResourceBounds = helpers.ResourceBounds{
		CPU: helpers.ResourceBound{
			Min: cpuMin,
			Max: cpuMax,
		},
		Memory: helpers.ResourceBound{
			Min: memMin,
			Max: memMax,
		},
	}
	return config
}

// GenerateRandomPolicyConfig generates a random policy configuration
func (g *PolicyConfigGenerator) GenerateRandomPolicyConfig(name string) helpers.PolicyConfig {
	modes := []v1alpha1.PolicyMode{
		v1alpha1.ModeAuto,
		v1alpha1.ModeRecommend,
		v1alpha1.ModeDisabled,
	}

	cpuMins := []string{"50m", "100m", "200m", "500m"}
	cpuMaxs := []string{"1000m", "2000m", "4000m", "8000m"}
	memMins := []string{"64Mi", "128Mi", "256Mi", "512Mi"}
	memMaxs := []string{"1Gi", "2Gi", "4Gi", "8Gi"}

	return helpers.PolicyConfig{
		Name: name,
		Mode: modes[g.rand.Intn(len(modes))],
		NamespaceSelector: map[string]string{
			"environment": "test",
			"team":        fmt.Sprintf("team-%d", g.rand.Intn(5)),
		},
		WorkloadSelector: map[string]string{
			"optimize": "true",
			"tier":     fmt.Sprintf("tier-%d", g.rand.Intn(3)),
		},
		ResourceBounds: helpers.ResourceBounds{
			CPU: helpers.ResourceBound{
				Min: cpuMins[g.rand.Intn(len(cpuMins))],
				Max: cpuMaxs[g.rand.Intn(len(cpuMaxs))],
			},
			Memory: helpers.ResourceBound{
				Min: memMins[g.rand.Intn(len(memMins))],
				Max: memMaxs[g.rand.Intn(len(memMaxs))],
			},
		},
		MetricsConfig: helpers.MetricsConfig{
			Provider:      "metrics-server",
			RollingWindow: fmt.Sprintf("%dm", 30+g.rand.Intn(90)), // 30-120 minutes
			Percentile:    []string{"P50", "P90", "P95", "P99"}[g.rand.Intn(4)],
			SafetyFactor:  1.1 + g.rand.Float64()*0.4, // 1.1-1.5
		},
		UpdateStrategy: helpers.UpdateStrategy{
			AllowInPlaceResize: g.rand.Intn(2) == 1,
			AllowRecreate:      g.rand.Intn(2) == 1,
			UpdateRequestsOnly: g.rand.Intn(2) == 1,
		},
		ReconciliationInterval: &metav1.Duration{
			Duration: time.Duration(1+g.rand.Intn(5)) * time.Minute,
		},
	}
}

// WorkloadConfigGenerator generates workload configurations for testing
type WorkloadConfigGenerator struct {
	rand *rand.Rand
}

// NewWorkloadConfigGenerator creates a new WorkloadConfigGenerator
func NewWorkloadConfigGenerator() *WorkloadConfigGenerator {
	return &WorkloadConfigGenerator{
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// GenerateBasicWorkloadConfig generates a basic workload configuration
func (g *WorkloadConfigGenerator) GenerateBasicWorkloadConfig(
	name string,
	workloadType helpers.WorkloadType,
) helpers.WorkloadConfig {
	replicas := int32(1)
	if workloadType == helpers.WorkloadTypeDaemonSet {
		replicas = 0 // DaemonSets don't use replicas
	}

	return helpers.WorkloadConfig{
		Name:      name,
		Namespace: "test-workloads",
		Type:      workloadType,
		Labels: map[string]string{
			"optimize": "true",
			"app":      name,
		},
		Resources: helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    "500m",
				Memory: "512Mi",
			},
			Limits: helpers.ResourceList{
				CPU:    "1000m",
				Memory: "1Gi",
			},
		},
		Replicas: replicas,
		Image:    "nginx:1.25-alpine",
	}
}

// GenerateWorkloadWithResources generates a workload with specific resource requirements
func (g *WorkloadConfigGenerator) GenerateWorkloadWithResources(
	name string,
	workloadType helpers.WorkloadType,
	cpuReq, memReq, cpuLim, memLim string,
) helpers.WorkloadConfig {
	config := g.GenerateBasicWorkloadConfig(name, workloadType)
	config.Resources = helpers.ResourceRequirements{
		Requests: helpers.ResourceList{
			CPU:    cpuReq,
			Memory: memReq,
		},
		Limits: helpers.ResourceList{
			CPU:    cpuLim,
			Memory: memLim,
		},
	}
	return config
}

// GenerateRandomWorkloadConfig generates a random workload configuration
func (g *WorkloadConfigGenerator) GenerateRandomWorkloadConfig(name string) helpers.WorkloadConfig {
	workloadTypes := []helpers.WorkloadType{
		helpers.WorkloadTypeDeployment,
		helpers.WorkloadTypeStatefulSet,
		helpers.WorkloadTypeDaemonSet,
	}

	cpuRequests := []string{"100m", "200m", "500m", "1000m"}
	memRequests := []string{"128Mi", "256Mi", "512Mi", "1Gi"}
	cpuLimits := []string{"500m", "1000m", "2000m", "4000m"}
	memLimits := []string{"256Mi", "512Mi", "1Gi", "2Gi"}
	images := []string{"nginx:1.25-alpine", "busybox:1.36", "alpine:3.18"}

	workloadType := workloadTypes[g.rand.Intn(len(workloadTypes))]
	replicas := int32(1 + g.rand.Intn(3))
	if workloadType == helpers.WorkloadTypeDaemonSet {
		replicas = 0
	}

	return helpers.WorkloadConfig{
		Name:      name,
		Namespace: "test-workloads",
		Type:      workloadType,
		Labels: map[string]string{
			"optimize": "true",
			"app":      name,
			"tier":     fmt.Sprintf("tier-%d", g.rand.Intn(3)),
		},
		Resources: helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    cpuRequests[g.rand.Intn(len(cpuRequests))],
				Memory: memRequests[g.rand.Intn(len(memRequests))],
			},
			Limits: helpers.ResourceList{
				CPU:    cpuLimits[g.rand.Intn(len(cpuLimits))],
				Memory: memLimits[g.rand.Intn(len(memLimits))],
			},
		},
		Replicas: replicas,
		Image:    images[g.rand.Intn(len(images))],
	}
}

// GenerateMultiContainerWorkloadConfig generates a workload configuration with multiple containers
func (g *WorkloadConfigGenerator) GenerateMultiContainerWorkloadConfig(name string, containerCount int) helpers.WorkloadConfig {
	config := g.GenerateBasicWorkloadConfig(name, helpers.WorkloadTypeDeployment)

	containers := make([]helpers.ContainerConfig, containerCount)
	images := []string{"nginx:1.25-alpine", "busybox:1.36", "alpine:3.18", "redis:7-alpine"}
	cpuRequests := []string{"50m", "100m", "200m", "300m"}
	memRequests := []string{"64Mi", "128Mi", "256Mi", "384Mi"}
	cpuLimits := []string{"200m", "500m", "1000m", "1500m"}
	memLimits := []string{"128Mi", "256Mi", "512Mi", "768Mi"}

	for i := 0; i < containerCount; i++ {
		containers[i] = helpers.ContainerConfig{
			Name:  fmt.Sprintf("%s-container-%d", name, i),
			Image: images[g.rand.Intn(len(images))],
			Resources: helpers.ResourceRequirements{
				Requests: helpers.ResourceList{
					CPU:    cpuRequests[g.rand.Intn(len(cpuRequests))],
					Memory: memRequests[g.rand.Intn(len(memRequests))],
				},
				Limits: helpers.ResourceList{
					CPU:    cpuLimits[g.rand.Intn(len(cpuLimits))],
					Memory: memLimits[g.rand.Intn(len(memLimits))],
				},
			},
		}
	}

	config.Containers = containers
	config.Labels["multi-container"] = TrueString
	config.Labels["container-count"] = fmt.Sprintf("%d", containerCount)

	return config
}

// TestScenario represents a complete test scenario with setup, execution, and validation
type TestScenario struct {
	Name        string
	Description string
	Policy      helpers.PolicyConfig
	Workload    helpers.WorkloadConfig
	Expected    ScenarioExpectation
	EdgeCase    bool
	Tags        []string
}

// ScenarioExpectation defines what behavior is expected from a test scenario
type ScenarioExpectation struct {
	ShouldApplyUpdates            bool
	ShouldGenerateRecommendations bool
	ShouldRespectBounds           bool
	ShouldHandleError             bool
	ExpectedErrorType             string
	ExpectedAnnotations           map[string]string
}

// BoundsTestCaseGenerator generates test cases for resource bounds testing
type BoundsTestCaseGenerator struct {
	rand *rand.Rand
}

// NewBoundsTestCaseGenerator creates a new BoundsTestCaseGenerator
func NewBoundsTestCaseGenerator() *BoundsTestCaseGenerator {
	return &BoundsTestCaseGenerator{
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// BoundsTestCase represents a test case for bounds enforcement
type BoundsTestCase struct {
	Name              string
	PolicyBounds      helpers.ResourceBounds
	WorkloadResources helpers.ResourceRequirements
	ExpectedBehavior  BoundsExpectation
}

// BoundsExpectation defines expected bounds enforcement behavior
type BoundsExpectation string

const (
	BoundsWithin     BoundsExpectation = "within"
	BoundsClampedMin BoundsExpectation = "clamped-to-min"
	BoundsClampedMax BoundsExpectation = "clamped-to-max"
)

// GenerateWithinBoundsTestCase generates a test case where resources are within bounds
func (g *BoundsTestCaseGenerator) GenerateWithinBoundsTestCase() BoundsTestCase {
	return BoundsTestCase{
		Name: "within-bounds",
		PolicyBounds: helpers.ResourceBounds{
			CPU: helpers.ResourceBound{
				Min: "200m",
				Max: "1000m",
			},
			Memory: helpers.ResourceBound{
				Min: "256Mi",
				Max: "1Gi",
			},
		},
		WorkloadResources: helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    "500m",
				Memory: "512Mi",
			},
		},
		ExpectedBehavior: BoundsWithin,
	}
}

// GenerateBelowMinBoundsTestCase generates a test case where resources are below minimum
func (g *BoundsTestCaseGenerator) GenerateBelowMinBoundsTestCase() BoundsTestCase {
	return BoundsTestCase{
		Name: "below-min-bounds",
		PolicyBounds: helpers.ResourceBounds{
			CPU: helpers.ResourceBound{
				Min: "500m",
				Max: "2000m",
			},
			Memory: helpers.ResourceBound{
				Min: "512Mi",
				Max: "2Gi",
			},
		},
		WorkloadResources: helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    "100m",  // Below min
				Memory: "128Mi", // Below min
			},
		},
		ExpectedBehavior: BoundsClampedMin,
	}
}

// GenerateAboveMaxBoundsTestCase generates a test case where resources are above maximum
func (g *BoundsTestCaseGenerator) GenerateAboveMaxBoundsTestCase() BoundsTestCase {
	return BoundsTestCase{
		Name: "above-max-bounds",
		PolicyBounds: helpers.ResourceBounds{
			CPU: helpers.ResourceBound{
				Min: "100m",
				Max: "500m",
			},
			Memory: helpers.ResourceBound{
				Min: "128Mi",
				Max: "512Mi",
			},
		},
		WorkloadResources: helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    "2000m", // Above max
				Memory: "2Gi",   // Above max
			},
		},
		ExpectedBehavior: BoundsClampedMax,
	}
}

// GenerateRandomBoundsTestCases generates multiple random bounds test cases
func (g *BoundsTestCaseGenerator) GenerateRandomBoundsTestCases(count int) []BoundsTestCase {
	testCases := make([]BoundsTestCase, count)

	for i := 0; i < count; i++ {
		testCases[i] = g.generateRandomBoundsTestCase(fmt.Sprintf("random-bounds-%d", i))
	}

	return testCases
}

// generateRandomBoundsTestCase generates a single random bounds test case
func (g *BoundsTestCaseGenerator) generateRandomBoundsTestCase(name string) BoundsTestCase {
	expectations := []BoundsExpectation{BoundsWithin, BoundsClampedMin, BoundsClampedMax}
	expectation := expectations[g.rand.Intn(len(expectations))]

	var policyBounds helpers.ResourceBounds
	var workloadResources helpers.ResourceRequirements

	switch expectation {
	case BoundsWithin:
		policyBounds = helpers.ResourceBounds{
			CPU:    helpers.ResourceBound{Min: "200m", Max: "1000m"},
			Memory: helpers.ResourceBound{Min: "256Mi", Max: "1Gi"},
		}
		workloadResources = helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    []string{"300m", "500m", "800m"}[g.rand.Intn(3)],
				Memory: []string{"384Mi", "512Mi", "768Mi"}[g.rand.Intn(3)],
			},
		}

	case BoundsClampedMin:
		policyBounds = helpers.ResourceBounds{
			CPU:    helpers.ResourceBound{Min: "500m", Max: "2000m"},
			Memory: helpers.ResourceBound{Min: "512Mi", Max: "2Gi"},
		}
		workloadResources = helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    []string{"100m", "200m", "300m"}[g.rand.Intn(3)],
				Memory: []string{"128Mi", "256Mi", "384Mi"}[g.rand.Intn(3)],
			},
		}

	case BoundsClampedMax:
		policyBounds = helpers.ResourceBounds{
			CPU:    helpers.ResourceBound{Min: "100m", Max: "500m"},
			Memory: helpers.ResourceBound{Min: "128Mi", Max: "512Mi"},
		}
		workloadResources = helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    []string{"1000m", "2000m", "4000m"}[g.rand.Intn(3)],
				Memory: []string{"1Gi", "2Gi", "4Gi"}[g.rand.Intn(3)],
			},
		}
	}

	return BoundsTestCase{
		Name:              name,
		PolicyBounds:      policyBounds,
		WorkloadResources: workloadResources,
		ExpectedBehavior:  expectation,
	}
}

// EdgeCaseScenarioGenerator generates edge case test scenarios
type EdgeCaseScenarioGenerator struct {
	rand *rand.Rand
}

// NewEdgeCaseScenarioGenerator creates a new EdgeCaseScenarioGenerator
func NewEdgeCaseScenarioGenerator() *EdgeCaseScenarioGenerator {
	return &EdgeCaseScenarioGenerator{
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// generateBasicPolicy generates a basic policy configuration for edge case testing
func (g *EdgeCaseScenarioGenerator) generateBasicPolicy(name string) helpers.PolicyConfig {
	return helpers.PolicyConfig{
		Name: name,
		Mode: v1alpha1.ModeRecommend,
		NamespaceSelector: map[string]string{
			"environment": "test",
		},
		WorkloadSelector: map[string]string{
			"optimize": "true",
		},
		ResourceBounds: helpers.ResourceBounds{
			CPU: helpers.ResourceBound{
				Min: "100m",
				Max: "2000m",
			},
			Memory: helpers.ResourceBound{
				Min: "128Mi",
				Max: "2Gi",
			},
		},
		MetricsConfig: helpers.MetricsConfig{
			Provider:      "metrics-server",
			RollingWindow: "1h",
			Percentile:    "P90",
			SafetyFactor:  1.2,
		},
		UpdateStrategy: helpers.UpdateStrategy{
			AllowInPlaceResize: true,
			UpdateRequestsOnly: true,
		},
	}
}

// generateBasicWorkload generates a basic workload configuration for edge case testing
func (g *EdgeCaseScenarioGenerator) generateBasicWorkload(name string) helpers.WorkloadConfig {
	return helpers.WorkloadConfig{
		Name:      name,
		Namespace: "test-workloads",
		Type:      helpers.WorkloadTypeDeployment,
		Labels: map[string]string{
			"optimize": "true",
			"app":      name,
		},
		Resources: helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    "200m",
				Memory: "256Mi",
			},
			Limits: helpers.ResourceList{
				CPU:    "500m",
				Memory: "512Mi",
			},
		},
		Replicas: 1,
		Image:    "nginx:1.25-alpine",
	}
}

// GenerateInvalidConfigurationScenarios generates scenarios with invalid configurations
func (g *EdgeCaseScenarioGenerator) GenerateInvalidConfigurationScenarios() []TestScenario {
	scenarios := []TestScenario{
		{
			Name:        "invalid-min-greater-than-max-cpu",
			Description: "Policy with CPU min greater than max should be rejected",
			Policy: helpers.PolicyConfig{
				Name: "invalid-cpu-bounds",
				Mode: v1alpha1.ModeRecommend,
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "2000m", // Greater than max
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "256Mi",
						Max: "1Gi",
					},
				},
			},
			Workload: g.generateBasicWorkload("test-workload-invalid-cpu"),
			Expected: ScenarioExpectation{
				ShouldHandleError: true,
				ExpectedErrorType: "validation-error",
			},
			EdgeCase: true,
			Tags:     []string{"invalid-config", "bounds-validation"},
		},
		{
			Name:        "workload-with-zero-resources",
			Description: "Workload with zero resource requests should be handled gracefully",
			Policy:      g.generateBasicPolicy("zero-resources-policy"),
			Workload: helpers.WorkloadConfig{
				Name:      "zero-resources-workload",
				Namespace: "test-workloads",
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"optimize":  "true",
					"edge-case": "zero-resources",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "0m",
						Memory: "0Mi",
					},
				},
				Replicas: 1,
				Image:    "nginx:1.25-alpine",
			},
			Expected: ScenarioExpectation{
				ShouldGenerateRecommendations: true,
				ShouldRespectBounds:           true,
			},
			EdgeCase: true,
			Tags:     []string{"edge-case", "zero-resources"},
		},
	}

	return scenarios
}

// GenerateMemorySafetyScenarios generates scenarios for testing memory decrease safety
func (g *EdgeCaseScenarioGenerator) GenerateMemorySafetyScenarios() []TestScenario {
	scenarios := []TestScenario{
		{
			Name:        "unsafe-memory-decrease",
			Description: "Large memory decrease should be flagged as unsafe",
			Policy:      g.generateBasicPolicy("memory-safety-policy"),
			Workload: helpers.WorkloadConfig{
				Name:      "high-memory-workload",
				Namespace: "test-workloads",
				Type:      helpers.WorkloadTypeDeployment,
				Labels: map[string]string{
					"optimize":    "true",
					"memory-test": "high",
				},
				Resources: helpers.ResourceRequirements{
					Requests: helpers.ResourceList{
						CPU:    "500m",
						Memory: "4Gi", // High memory that might be decreased
					},
					Limits: helpers.ResourceList{
						CPU:    "1000m",
						Memory: "8Gi",
					},
				},
				Replicas: 1,
				Image:    "nginx:1.25-alpine",
			},
			Expected: ScenarioExpectation{
				ShouldGenerateRecommendations: true,
				ExpectedAnnotations: map[string]string{
					"optipod.io/memory-decrease-warning": "true",
				},
			},
			EdgeCase: true,
			Tags:     []string{"memory-safety", "unsafe-decrease"},
		},
	}

	return scenarios
}

// GenerateConcurrentModificationScenarios generates scenarios for testing concurrent modifications
func (g *EdgeCaseScenarioGenerator) GenerateConcurrentModificationScenarios() []TestScenario {
	scenarios := []TestScenario{
		{
			Name:        "concurrent-policy-updates",
			Description: "Multiple policies targeting the same workload should be handled correctly",
			Policy:      g.generateBasicPolicy("concurrent-policy-1"),
			Workload:    g.generateBasicWorkload("concurrent-target-workload"),
			Expected: ScenarioExpectation{
				ShouldGenerateRecommendations: true,
				ShouldRespectBounds:           true,
			},
			EdgeCase: true,
			Tags:     []string{"concurrent", "policy-conflicts"},
		},
	}

	return scenarios
}

// GenerateRBACScenarios generates scenarios for testing RBAC and security constraints
func (g *EdgeCaseScenarioGenerator) GenerateRBACScenarios() []TestScenario {
	scenarios := []TestScenario{
		{
			Name:        "restricted-service-account",
			Description: "OptipPod with restricted permissions should handle errors gracefully",
			Policy:      g.generateBasicPolicy("rbac-restricted-policy"),
			Workload:    g.generateBasicWorkload("rbac-test-workload"),
			Expected: ScenarioExpectation{
				ShouldHandleError: true,
				ExpectedErrorType: "permission-denied",
			},
			EdgeCase: true,
			Tags:     []string{"rbac", "security", "restricted-permissions"},
		},
	}

	return scenarios
}

// TestScenarioGenerator generates complete test scenarios
type TestScenarioGenerator struct {
	policyGen   *PolicyConfigGenerator
	workloadGen *WorkloadConfigGenerator
	boundsGen   *BoundsTestCaseGenerator
}

// NewTestScenarioGenerator creates a new TestScenarioGenerator
func NewTestScenarioGenerator() *TestScenarioGenerator {
	return &TestScenarioGenerator{
		policyGen:   NewPolicyConfigGenerator(),
		workloadGen: NewWorkloadConfigGenerator(),
		boundsGen:   NewBoundsTestCaseGenerator(),
	}
}

// GeneratePolicyModeScenario generates a complete policy mode test scenario
func (g *TestScenarioGenerator) GeneratePolicyModeScenario(mode v1alpha1.PolicyMode) (helpers.PolicyConfig, helpers.WorkloadConfig) {
	policyName := fmt.Sprintf("test-policy-%s", toLowerCase(string(mode)))
	workloadName := fmt.Sprintf("test-workload-%s", toLowerCase(string(mode)))

	policy := g.policyGen.GenerateBasicPolicyConfig(policyName, mode)
	workload := g.workloadGen.GenerateBasicWorkloadConfig(workloadName, helpers.WorkloadTypeDeployment)

	// Adjust workload labels for mode-specific testing
	switch mode {
	case v1alpha1.ModeAuto:
		workload.Labels["auto-update"] = TrueString
	case v1alpha1.ModeDisabled:
		workload.Labels["test-disabled"] = "true"
	}

	return policy, workload
}

// GenerateResourceBoundsScenario generates a complete resource bounds test scenario
func (g *TestScenarioGenerator) GenerateResourceBoundsScenario(expectation BoundsExpectation) (helpers.PolicyConfig, helpers.WorkloadConfig) {
	var testCase BoundsTestCase

	switch expectation {
	case BoundsWithin:
		testCase = g.boundsGen.GenerateWithinBoundsTestCase()
	case BoundsClampedMin:
		testCase = g.boundsGen.GenerateBelowMinBoundsTestCase()
	case BoundsClampedMax:
		testCase = g.boundsGen.GenerateAboveMaxBoundsTestCase()
	}

	policy := g.policyGen.GenerateBasicPolicyConfig(
		fmt.Sprintf("bounds-policy-%s", testCase.Name),
		v1alpha1.ModeRecommend,
	)
	policy.ResourceBounds = testCase.PolicyBounds

	workload := g.workloadGen.GenerateBasicWorkloadConfig(
		fmt.Sprintf("bounds-workload-%s", testCase.Name),
		helpers.WorkloadTypeDeployment,
	)
	workload.Resources = testCase.WorkloadResources
	workload.Labels["test-bounds"] = "true"

	return policy, workload
}

// Helper function to convert string to lowercase
func toLowerCase(s string) string {
	result := make([]rune, len(s))
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			result[i] = r + 32
		} else {
			result[i] = r
		}
	}
	return string(result)
}

// RandomizedTestDataGenerator generates randomized test data for comprehensive testing
type RandomizedTestDataGenerator struct {
	rand *rand.Rand
}

// NewRandomizedTestDataGenerator creates a new RandomizedTestDataGenerator
func NewRandomizedTestDataGenerator() *RandomizedTestDataGenerator {
	return &RandomizedTestDataGenerator{
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// GenerateRandomTestScenarios generates a specified number of random test scenarios
func (g *RandomizedTestDataGenerator) GenerateRandomTestScenarios(count int) []TestScenario {
	scenarios := make([]TestScenario, count)

	for i := 0; i < count; i++ {
		scenarios[i] = g.generateRandomTestScenario(fmt.Sprintf("random-scenario-%d", i))
	}

	return scenarios
}

// generateRandomTestScenario generates a single random test scenario
func (g *RandomizedTestDataGenerator) generateRandomTestScenario(name string) TestScenario {
	modes := []v1alpha1.PolicyMode{
		v1alpha1.ModeAuto,
		v1alpha1.ModeRecommend,
		v1alpha1.ModeDisabled,
	}

	workloadTypes := []helpers.WorkloadType{
		helpers.WorkloadTypeDeployment,
		helpers.WorkloadTypeStatefulSet,
		helpers.WorkloadTypeDaemonSet,
	}

	mode := modes[g.rand.Intn(len(modes))]
	workloadType := workloadTypes[g.rand.Intn(len(workloadTypes))]

	policy := g.generateRandomPolicy(fmt.Sprintf("%s-policy", name), mode)
	workload := g.generateRandomWorkload(fmt.Sprintf("%s-workload", name), workloadType)

	expected := ScenarioExpectation{
		ShouldApplyUpdates:            mode == v1alpha1.ModeAuto,
		ShouldGenerateRecommendations: mode != v1alpha1.ModeDisabled,
		ShouldRespectBounds:           true,
	}

	return TestScenario{
		Name:        name,
		Description: fmt.Sprintf("Random test scenario with %s mode and %s workload", mode, workloadType),
		Policy:      policy,
		Workload:    workload,
		Expected:    expected,
		EdgeCase:    false,
		Tags:        []string{"random", "comprehensive"},
	}
}

// generateRandomPolicy generates a random policy configuration
func (g *RandomizedTestDataGenerator) generateRandomPolicy(name string, mode v1alpha1.PolicyMode) helpers.PolicyConfig {
	cpuMins := []string{"50m", "100m", "200m", "500m"}
	cpuMaxs := []string{"1000m", "2000m", "4000m", "8000m"}
	memMins := []string{"64Mi", "128Mi", "256Mi", "512Mi"}
	memMaxs := []string{"1Gi", "2Gi", "4Gi", "8Gi"}

	return helpers.PolicyConfig{
		Name: name,
		Mode: mode,
		NamespaceSelector: map[string]string{
			"environment": "test",
			"team":        fmt.Sprintf("team-%d", g.rand.Intn(5)),
		},
		WorkloadSelector: map[string]string{
			"optimize": "true",
			"tier":     fmt.Sprintf("tier-%d", g.rand.Intn(3)),
		},
		ResourceBounds: helpers.ResourceBounds{
			CPU: helpers.ResourceBound{
				Min: cpuMins[g.rand.Intn(len(cpuMins))],
				Max: cpuMaxs[g.rand.Intn(len(cpuMaxs))],
			},
			Memory: helpers.ResourceBound{
				Min: memMins[g.rand.Intn(len(memMins))],
				Max: memMaxs[g.rand.Intn(len(memMaxs))],
			},
		},
		MetricsConfig: helpers.MetricsConfig{
			Provider:      "metrics-server",
			RollingWindow: fmt.Sprintf("%dm", 30+g.rand.Intn(90)),
			Percentile:    []string{"P50", "P90", "P95", "P99"}[g.rand.Intn(4)],
			SafetyFactor:  1.1 + g.rand.Float64()*0.4,
		},
		UpdateStrategy: helpers.UpdateStrategy{
			AllowInPlaceResize: g.rand.Intn(2) == 1,
			AllowRecreate:      g.rand.Intn(2) == 1,
			UpdateRequestsOnly: g.rand.Intn(2) == 1,
		},
	}
}

// generateRandomWorkload generates a random workload configuration
func (g *RandomizedTestDataGenerator) generateRandomWorkload(name string, workloadType helpers.WorkloadType) helpers.WorkloadConfig {
	cpuRequests := []string{"100m", "200m", "500m", "1000m"}
	memRequests := []string{"128Mi", "256Mi", "512Mi", "1Gi"}
	cpuLimits := []string{"500m", "1000m", "2000m", "4000m"}
	memLimits := []string{"256Mi", "512Mi", "1Gi", "2Gi"}
	images := []string{"nginx:1.25-alpine", "busybox:1.36", "alpine:3.18"}

	replicas := int32(1 + g.rand.Intn(3))
	if workloadType == helpers.WorkloadTypeDaemonSet {
		replicas = 0
	}

	return helpers.WorkloadConfig{
		Name:      name,
		Namespace: "test-workloads",
		Type:      workloadType,
		Labels: map[string]string{
			"optimize": "true",
			"app":      name,
			"tier":     fmt.Sprintf("tier-%d", g.rand.Intn(3)),
		},
		Resources: helpers.ResourceRequirements{
			Requests: helpers.ResourceList{
				CPU:    cpuRequests[g.rand.Intn(len(cpuRequests))],
				Memory: memRequests[g.rand.Intn(len(memRequests))],
			},
			Limits: helpers.ResourceList{
				CPU:    cpuLimits[g.rand.Intn(len(cpuLimits))],
				Memory: memLimits[g.rand.Intn(len(memLimits))],
			},
		},
		Replicas: replicas,
		Image:    images[g.rand.Intn(len(images))],
	}
}

// ScenarioValidationHelper provides validation functions for test scenarios
type ScenarioValidationHelper struct{}

// NewScenarioValidationHelper creates a new ScenarioValidationHelper
func NewScenarioValidationHelper() *ScenarioValidationHelper {
	return &ScenarioValidationHelper{}
}

// ValidateScenario validates that a test scenario is properly configured
func (h *ScenarioValidationHelper) ValidateScenario(scenario TestScenario) error {
	// Validate policy configuration
	if err := h.validatePolicyConfig(scenario.Policy); err != nil {
		return fmt.Errorf("invalid policy configuration: %w", err)
	}

	// Validate workload configuration
	if err := h.validateWorkloadConfig(scenario.Workload); err != nil {
		return fmt.Errorf("invalid workload configuration: %w", err)
	}

	// Validate expectations are consistent with configuration
	if err := h.validateExpectations(scenario.Policy, scenario.Expected); err != nil {
		return fmt.Errorf("invalid expectations: %w", err)
	}

	return nil
}

// validatePolicyConfig validates a policy configuration
func (h *ScenarioValidationHelper) validatePolicyConfig(policy helpers.PolicyConfig) error {
	if policy.Name == "" {
		return fmt.Errorf("policy name cannot be empty")
	}
	return nil
}

// validateWorkloadConfig validates a workload configuration
func (h *ScenarioValidationHelper) validateWorkloadConfig(workload helpers.WorkloadConfig) error {
	if workload.Name == "" {
		return fmt.Errorf("workload name cannot be empty")
	}

	if workload.Type == "" {
		return fmt.Errorf("workload type cannot be empty")
	}

	if workload.Type == helpers.WorkloadTypeDaemonSet && workload.Replicas != 0 {
		return fmt.Errorf("DaemonSet replicas should be 0")
	}

	if workload.Image == "" {
		return fmt.Errorf("workload image cannot be empty")
	}

	return nil
}

// validateExpectations validates that expectations are consistent with policy configuration
func (h *ScenarioValidationHelper) validateExpectations(policy helpers.PolicyConfig, expected ScenarioExpectation) error {
	// Auto mode should apply updates
	if policy.Mode == v1alpha1.ModeAuto && !expected.ShouldApplyUpdates {
		return fmt.Errorf("Auto mode should apply updates")
	}

	// Disabled mode should not generate recommendations
	if policy.Mode == v1alpha1.ModeDisabled && expected.ShouldGenerateRecommendations {
		return fmt.Errorf("Disabled mode should not generate recommendations")
	}

	// Recommend mode should generate recommendations but not apply updates
	if policy.Mode == v1alpha1.ModeRecommend {
		if !expected.ShouldGenerateRecommendations {
			return fmt.Errorf("Recommend mode should generate recommendations")
		}
		if expected.ShouldApplyUpdates {
			return fmt.Errorf("Recommend mode should not apply updates")
		}
	}

	return nil
}

// ComprehensiveScenarioGenerator combines all scenario generators for comprehensive testing
type ComprehensiveScenarioGenerator struct {
	edgeCaseGen      *EdgeCaseScenarioGenerator
	randomizedGen    *RandomizedTestDataGenerator
	validationHelper *ScenarioValidationHelper
}

// NewComprehensiveScenarioGenerator creates a new ComprehensiveScenarioGenerator
func NewComprehensiveScenarioGenerator() *ComprehensiveScenarioGenerator {
	return &ComprehensiveScenarioGenerator{
		edgeCaseGen:      NewEdgeCaseScenarioGenerator(),
		randomizedGen:    NewRandomizedTestDataGenerator(),
		validationHelper: NewScenarioValidationHelper(),
	}
}

// GenerateAllScenarios generates a comprehensive set of test scenarios
func (g *ComprehensiveScenarioGenerator) GenerateAllScenarios() []TestScenario {
	var allScenarios []TestScenario

	// Add edge case scenarios
	allScenarios = append(allScenarios, g.edgeCaseGen.GenerateInvalidConfigurationScenarios()...)
	allScenarios = append(allScenarios, g.edgeCaseGen.GenerateMemorySafetyScenarios()...)

	// Add randomized scenarios
	allScenarios = append(allScenarios, g.randomizedGen.GenerateRandomTestScenarios(5)...)

	// Validate all scenarios
	validScenarios := make([]TestScenario, 0, len(allScenarios))
	for _, scenario := range allScenarios {
		if err := g.validationHelper.ValidateScenario(scenario); err == nil {
			validScenarios = append(validScenarios, scenario)
		}
	}

	return validScenarios
}

// GenerateScenariosByTag generates scenarios filtered by specific tags
func (g *ComprehensiveScenarioGenerator) GenerateScenariosByTag(tags []string) []TestScenario {
	allScenarios := g.GenerateAllScenarios()
	filteredScenarios := make([]TestScenario, 0)

	for _, scenario := range allScenarios {
		if g.hasAnyTag(scenario.Tags, tags) {
			filteredScenarios = append(filteredScenarios, scenario)
		}
	}

	return filteredScenarios
}

// hasAnyTag checks if a scenario has any of the specified tags
func (g *ComprehensiveScenarioGenerator) hasAnyTag(scenarioTags, filterTags []string) bool {
	for _, filterTag := range filterTags {
		for _, scenarioTag := range scenarioTags {
			if scenarioTag == filterTag {
				return true
			}
		}
	}
	return false
}
