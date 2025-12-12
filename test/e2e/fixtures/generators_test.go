package fixtures

import (
	"testing"

	"github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/test/e2e/helpers"
)

const (
	// TrueString represents the string "true" used in labels and comparisons
	TrueString = "true"
)

func TestPolicyConfigGenerator(t *testing.T) {
	generator := NewPolicyConfigGenerator()

	t.Run("GenerateBasicPolicyConfig", func(t *testing.T) {
		config := generator.GenerateBasicPolicyConfig("test-policy", v1alpha1.ModeAuto)

		if config.Name != "test-policy" {
			t.Errorf("Expected name 'test-policy', got %s", config.Name)
		}

		if config.Mode != v1alpha1.ModeAuto {
			t.Errorf("Expected mode Auto, got %s", config.Mode)
		}

		if config.ResourceBounds.CPU.Min == "" {
			t.Error("Expected CPU min to be set")
		}

		if config.ResourceBounds.Memory.Max == "" {
			t.Error("Expected memory max to be set")
		}
	})

	t.Run("GeneratePolicyWithBounds", func(t *testing.T) {
		config := generator.GeneratePolicyWithBounds("bounds-policy", "200m", "1000m", "256Mi", "1Gi")

		if config.ResourceBounds.CPU.Min != "200m" {
			t.Errorf("Expected CPU min '200m', got %s", config.ResourceBounds.CPU.Min)
		}

		if config.ResourceBounds.CPU.Max != "1000m" {
			t.Errorf("Expected CPU max '1000m', got %s", config.ResourceBounds.CPU.Max)
		}

		if config.ResourceBounds.Memory.Min != "256Mi" {
			t.Errorf("Expected memory min '256Mi', got %s", config.ResourceBounds.Memory.Min)
		}

		if config.ResourceBounds.Memory.Max != "1Gi" {
			t.Errorf("Expected memory max '1Gi', got %s", config.ResourceBounds.Memory.Max)
		}
	})

	t.Run("GenerateRandomPolicyConfig", func(t *testing.T) {
		config := generator.GenerateRandomPolicyConfig("random-policy")

		if config.Name != "random-policy" {
			t.Errorf("Expected name 'random-policy', got %s", config.Name)
		}

		// Verify mode is one of the valid modes
		validModes := []v1alpha1.PolicyMode{v1alpha1.ModeAuto, v1alpha1.ModeRecommend, v1alpha1.ModeDisabled}
		modeValid := false
		for _, mode := range validModes {
			if config.Mode == mode {
				modeValid = true
				break
			}
		}
		if !modeValid {
			t.Errorf("Generated invalid mode: %s", config.Mode)
		}

		// Verify resource bounds are set
		if config.ResourceBounds.CPU.Min == "" || config.ResourceBounds.CPU.Max == "" {
			t.Error("CPU bounds should be set")
		}

		if config.ResourceBounds.Memory.Min == "" || config.ResourceBounds.Memory.Max == "" {
			t.Error("Memory bounds should be set")
		}
	})
}

func TestWorkloadConfigGenerator(t *testing.T) {
	generator := NewWorkloadConfigGenerator()

	t.Run("GenerateBasicWorkloadConfig", func(t *testing.T) {
		config := generator.GenerateBasicWorkloadConfig("test-workload", helpers.WorkloadTypeDeployment)

		if config.Name != "test-workload" {
			t.Errorf("Expected name 'test-workload', got %s", config.Name)
		}

		if config.Type != helpers.WorkloadTypeDeployment {
			t.Errorf("Expected type Deployment, got %s", config.Type)
		}

		if config.Replicas != 1 {
			t.Errorf("Expected replicas 1, got %d", config.Replicas)
		}

		if config.Resources.Requests.CPU == "" {
			t.Error("Expected CPU request to be set")
		}

		if config.Resources.Requests.Memory == "" {
			t.Error("Expected memory request to be set")
		}
	})

	t.Run("GenerateBasicWorkloadConfig_DaemonSet", func(t *testing.T) {
		config := generator.GenerateBasicWorkloadConfig("test-daemonset", helpers.WorkloadTypeDaemonSet)

		if config.Type != helpers.WorkloadTypeDaemonSet {
			t.Errorf("Expected type DaemonSet, got %s", config.Type)
		}

		if config.Replicas != 0 {
			t.Errorf("Expected replicas 0 for DaemonSet, got %d", config.Replicas)
		}
	})

	t.Run("GenerateWorkloadWithResources", func(t *testing.T) {
		config := generator.GenerateWorkloadWithResources("resource-workload", helpers.WorkloadTypeDeployment, "300m", "384Mi", "600m", "768Mi")

		if config.Resources.Requests.CPU != "300m" {
			t.Errorf("Expected CPU request '300m', got %s", config.Resources.Requests.CPU)
		}

		if config.Resources.Requests.Memory != "384Mi" {
			t.Errorf("Expected memory request '384Mi', got %s", config.Resources.Requests.Memory)
		}

		if config.Resources.Limits.CPU != "600m" {
			t.Errorf("Expected CPU limit '600m', got %s", config.Resources.Limits.CPU)
		}

		if config.Resources.Limits.Memory != "768Mi" {
			t.Errorf("Expected memory limit '768Mi', got %s", config.Resources.Limits.Memory)
		}
	})

	t.Run("GenerateRandomWorkloadConfig", func(t *testing.T) {
		config := generator.GenerateRandomWorkloadConfig("random-workload")

		if config.Name != "random-workload" {
			t.Errorf("Expected name 'random-workload', got %s", config.Name)
		}

		// Verify workload type is valid
		validTypes := []helpers.WorkloadType{helpers.WorkloadTypeDeployment, helpers.WorkloadTypeStatefulSet, helpers.WorkloadTypeDaemonSet}
		typeValid := false
		for _, wType := range validTypes {
			if config.Type == wType {
				typeValid = true
				break
			}
		}
		if !typeValid {
			t.Errorf("Generated invalid workload type: %s", config.Type)
		}

		// Verify resources are set
		if config.Resources.Requests.CPU == "" || config.Resources.Requests.Memory == "" {
			t.Error("Resource requests should be set")
		}

		// Verify DaemonSet has 0 replicas
		if config.Type == helpers.WorkloadTypeDaemonSet && config.Replicas != 0 {
			t.Errorf("DaemonSet should have 0 replicas, got %d", config.Replicas)
		}
	})

	t.Run("GenerateMultiContainerWorkloadConfig", func(t *testing.T) {
		config := generator.GenerateMultiContainerWorkloadConfig("multi-container", 3)

		if len(config.Containers) != 3 {
			t.Errorf("Expected 3 containers, got %d", len(config.Containers))
		}

		for i, container := range config.Containers {
			expectedName := "multi-container-container-" + string(rune('0'+i))
			if container.Name != expectedName {
				t.Errorf("Expected container name %s, got %s", expectedName, container.Name)
			}

			if container.Image == "" {
				t.Errorf("Container %d should have an image", i)
			}

			if container.Resources.Requests.CPU == "" || container.Resources.Requests.Memory == "" {
				t.Errorf("Container %d should have resource requests", i)
			}
		}

		if config.Labels["multi-container"] != TrueString {
			t.Error("Expected multi-container label to be true")
		}

		if config.Labels["container-count"] != "3" {
			t.Errorf("Expected container-count label to be '3', got %s", config.Labels["container-count"])
		}
	})
}

func TestBoundsTestCaseGenerator(t *testing.T) {
	generator := NewBoundsTestCaseGenerator()

	t.Run("GenerateWithinBoundsTestCase", func(t *testing.T) {
		testCase := generator.GenerateWithinBoundsTestCase()

		if testCase.Name != "within-bounds" {
			t.Errorf("Expected name 'within-bounds', got %s", testCase.Name)
		}

		if testCase.ExpectedBehavior != BoundsWithin {
			t.Errorf("Expected behavior BoundsWithin, got %s", testCase.ExpectedBehavior)
		}

		if testCase.PolicyBounds.CPU.Min == "" || testCase.PolicyBounds.CPU.Max == "" {
			t.Error("Policy CPU bounds should be set")
		}

		if testCase.WorkloadResources.Requests.CPU == "" || testCase.WorkloadResources.Requests.Memory == "" {
			t.Error("Workload resource requests should be set")
		}
	})

	t.Run("GenerateBelowMinBoundsTestCase", func(t *testing.T) {
		testCase := generator.GenerateBelowMinBoundsTestCase()

		if testCase.ExpectedBehavior != BoundsClampedMin {
			t.Errorf("Expected behavior BoundsClampedMin, got %s", testCase.ExpectedBehavior)
		}
	})

	t.Run("GenerateAboveMaxBoundsTestCase", func(t *testing.T) {
		testCase := generator.GenerateAboveMaxBoundsTestCase()

		if testCase.ExpectedBehavior != BoundsClampedMax {
			t.Errorf("Expected behavior BoundsClampedMax, got %s", testCase.ExpectedBehavior)
		}
	})

	t.Run("GenerateRandomBoundsTestCases", func(t *testing.T) {
		testCases := generator.GenerateRandomBoundsTestCases(5)

		if len(testCases) != 5 {
			t.Errorf("Expected 5 test cases, got %d", len(testCases))
		}

		for i, testCase := range testCases {
			if testCase.Name == "" {
				t.Errorf("Test case %d should have a name", i)
			}

			validBehaviors := []BoundsExpectation{BoundsWithin, BoundsClampedMin, BoundsClampedMax}
			behaviorValid := false
			for _, behavior := range validBehaviors {
				if testCase.ExpectedBehavior == behavior {
					behaviorValid = true
					break
				}
			}
			if !behaviorValid {
				t.Errorf("Test case %d has invalid behavior: %s", i, testCase.ExpectedBehavior)
			}
		}
	})
}

func TestTestScenarioGenerator(t *testing.T) {
	generator := NewTestScenarioGenerator()

	t.Run("GeneratePolicyModeScenario", func(t *testing.T) {
		policy, workload := generator.GeneratePolicyModeScenario(v1alpha1.ModeAuto)

		if policy.Mode != v1alpha1.ModeAuto {
			t.Errorf("Expected policy mode Auto, got %s", policy.Mode)
		}

		if workload.Labels["auto-update"] != TrueString {
			t.Error("Expected auto-update label for Auto mode")
		}
	})

	t.Run("GenerateResourceBoundsScenario", func(t *testing.T) {
		policy, workload := generator.GenerateResourceBoundsScenario(BoundsWithin)

		if policy.Mode != v1alpha1.ModeRecommend {
			t.Errorf("Expected policy mode Recommend, got %s", policy.Mode)
		}

		if workload.Labels["test-bounds"] != TrueString {
			t.Error("Expected test-bounds label")
		}
	})
}

func TestEdgeCaseScenarioGenerator(t *testing.T) {
	generator := NewEdgeCaseScenarioGenerator()

	t.Run("GenerateInvalidConfigurationScenarios", func(t *testing.T) {
		scenarios := generator.GenerateInvalidConfigurationScenarios()

		if len(scenarios) == 0 {
			t.Error("Expected at least one invalid configuration scenario")
		}

		for _, scenario := range scenarios {
			if !scenario.EdgeCase {
				t.Errorf("Scenario %s should be marked as edge case", scenario.Name)
			}

			if scenario.Name == "" {
				t.Error("Scenario should have a name")
			}

			if scenario.Description == "" {
				t.Error("Scenario should have a description")
			}
		}
	})

	t.Run("GenerateConcurrentModificationScenarios", func(t *testing.T) {
		scenarios := generator.GenerateConcurrentModificationScenarios()

		if len(scenarios) == 0 {
			t.Error("Expected at least one concurrent modification scenario")
		}

		for _, scenario := range scenarios {
			if !scenario.EdgeCase {
				t.Errorf("Scenario %s should be marked as edge case", scenario.Name)
			}

			hasTag := false
			for _, tag := range scenario.Tags {
				if tag == "concurrent" {
					hasTag = true
					break
				}
			}
			if !hasTag {
				t.Errorf("Scenario %s should have 'concurrent' tag", scenario.Name)
			}
		}
	})

	t.Run("GenerateMemorySafetyScenarios", func(t *testing.T) {
		scenarios := generator.GenerateMemorySafetyScenarios()

		if len(scenarios) == 0 {
			t.Error("Expected at least one memory safety scenario")
		}

		for _, scenario := range scenarios {
			if !scenario.EdgeCase {
				t.Errorf("Scenario %s should be marked as edge case", scenario.Name)
			}

			hasTag := false
			for _, tag := range scenario.Tags {
				if tag == "memory-safety" {
					hasTag = true
					break
				}
			}
			if !hasTag {
				t.Errorf("Scenario %s should have 'memory-safety' tag", scenario.Name)
			}
		}
	})

	t.Run("GenerateRBACScenarios", func(t *testing.T) {
		scenarios := generator.GenerateRBACScenarios()

		if len(scenarios) == 0 {
			t.Error("Expected at least one RBAC scenario")
		}

		for _, scenario := range scenarios {
			if !scenario.EdgeCase {
				t.Errorf("Scenario %s should be marked as edge case", scenario.Name)
			}

			hasTag := false
			for _, tag := range scenario.Tags {
				if tag == "rbac" {
					hasTag = true
					break
				}
			}
			if !hasTag {
				t.Errorf("Scenario %s should have 'rbac' tag", scenario.Name)
			}
		}
	})
}

func TestRandomizedTestDataGenerator(t *testing.T) {
	generator := NewRandomizedTestDataGenerator()

	t.Run("GenerateRandomTestScenarios", func(t *testing.T) {
		scenarios := generator.GenerateRandomTestScenarios(3)

		if len(scenarios) != 3 {
			t.Errorf("Expected 3 scenarios, got %d", len(scenarios))
		}

		for i, scenario := range scenarios {
			if scenario.Name == "" {
				t.Errorf("Scenario %d should have a name", i)
			}

			if scenario.Description == "" {
				t.Errorf("Scenario %d should have a description", i)
			}

			if scenario.EdgeCase {
				t.Errorf("Random scenario %d should not be marked as edge case", i)
			}

			// Verify expectations are consistent with policy mode
			switch scenario.Policy.Mode {
			case v1alpha1.ModeAuto:
				if !scenario.Expected.ShouldApplyUpdates {
					t.Errorf("Auto mode scenario %d should apply updates", i)
				}
				if !scenario.Expected.ShouldGenerateRecommendations {
					t.Errorf("Auto mode scenario %d should generate recommendations", i)
				}
			case v1alpha1.ModeRecommend:
				if scenario.Expected.ShouldApplyUpdates {
					t.Errorf("Recommend mode scenario %d should not apply updates", i)
				}
				if !scenario.Expected.ShouldGenerateRecommendations {
					t.Errorf("Recommend mode scenario %d should generate recommendations", i)
				}
			case v1alpha1.ModeDisabled:
				if scenario.Expected.ShouldApplyUpdates {
					t.Errorf("Disabled mode scenario %d should not apply updates", i)
				}
				if scenario.Expected.ShouldGenerateRecommendations {
					t.Errorf("Disabled mode scenario %d should not generate recommendations", i)
				}
			}
		}
	})
}

func TestScenarioValidationHelper(t *testing.T) {
	helper := NewScenarioValidationHelper()

	t.Run("ValidateScenario_Valid", func(t *testing.T) {
		scenario := TestScenario{
			Name:        "valid-scenario",
			Description: "A valid test scenario",
			Policy: helpers.PolicyConfig{
				Name: "valid-policy",
				Mode: v1alpha1.ModeRecommend,
				ResourceBounds: helpers.ResourceBounds{
					CPU: helpers.ResourceBound{
						Min: "100m",
						Max: "1000m",
					},
					Memory: helpers.ResourceBound{
						Min: "128Mi",
						Max: "1Gi",
					},
				},
			},
			Workload: helpers.WorkloadConfig{
				Name:     "valid-workload",
				Type:     helpers.WorkloadTypeDeployment,
				Replicas: 1,
				Image:    "nginx:1.25-alpine",
			},
			Expected: ScenarioExpectation{
				ShouldGenerateRecommendations: true,
				ShouldApplyUpdates:            false,
			},
		}

		err := helper.ValidateScenario(scenario)
		if err != nil {
			t.Errorf("Valid scenario should not return error: %v", err)
		}
	})

	t.Run("ValidateScenario_InvalidPolicy", func(t *testing.T) {
		scenario := TestScenario{
			Policy: helpers.PolicyConfig{
				Name: "", // Invalid: empty name
			},
			Workload: helpers.WorkloadConfig{
				Name:  "valid-workload",
				Type:  helpers.WorkloadTypeDeployment,
				Image: "nginx:1.25-alpine",
			},
		}

		err := helper.ValidateScenario(scenario)
		if err == nil {
			t.Error("Invalid policy should return error")
		}
	})

	t.Run("ValidateScenario_InvalidWorkload", func(t *testing.T) {
		scenario := TestScenario{
			Policy: helpers.PolicyConfig{
				Name: "valid-policy",
				Mode: v1alpha1.ModeRecommend,
			},
			Workload: helpers.WorkloadConfig{
				Name: "", // Invalid: empty name
				Type: helpers.WorkloadTypeDeployment,
			},
		}

		err := helper.ValidateScenario(scenario)
		if err == nil {
			t.Error("Invalid workload should return error")
		}
	})

	t.Run("ValidateScenario_InconsistentExpectations", func(t *testing.T) {
		scenario := TestScenario{
			Policy: helpers.PolicyConfig{
				Name: "auto-policy",
				Mode: v1alpha1.ModeAuto,
			},
			Workload: helpers.WorkloadConfig{
				Name:  "valid-workload",
				Type:  helpers.WorkloadTypeDeployment,
				Image: "nginx:1.25-alpine",
			},
			Expected: ScenarioExpectation{
				ShouldApplyUpdates: false, // Inconsistent: Auto mode should apply updates
			},
		}

		err := helper.ValidateScenario(scenario)
		if err == nil {
			t.Error("Inconsistent expectations should return error")
		}
	})
}

func TestComprehensiveScenarioGenerator(t *testing.T) {
	generator := NewComprehensiveScenarioGenerator()

	t.Run("GenerateAllScenarios", func(t *testing.T) {
		scenarios := generator.GenerateAllScenarios()

		if len(scenarios) == 0 {
			t.Error("Expected at least one scenario")
		}

		// Verify we have both edge cases and random scenarios
		hasEdgeCase := false
		hasRandom := false

		for _, scenario := range scenarios {
			if scenario.EdgeCase {
				hasEdgeCase = true
			}
			for _, tag := range scenario.Tags {
				if tag == "random" {
					hasRandom = true
					break
				}
			}
		}

		if !hasEdgeCase {
			t.Error("Expected at least one edge case scenario")
		}

		if !hasRandom {
			t.Error("Expected at least one random scenario")
		}
	})

	t.Run("GenerateScenariosByTag", func(t *testing.T) {
		scenarios := generator.GenerateScenariosByTag([]string{"memory-safety"})

		if len(scenarios) == 0 {
			t.Error("Expected at least one memory-safety scenario")
		}

		for _, scenario := range scenarios {
			hasTag := false
			for _, tag := range scenario.Tags {
				if tag == "memory-safety" {
					hasTag = true
					break
				}
			}
			if !hasTag {
				t.Errorf("Scenario %s should have 'memory-safety' tag", scenario.Name)
			}
		}
	})
}
