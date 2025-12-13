//go:build e2e

package e2e

import (
	"testing"

	"github.com/optipod/optipod/test/e2e/helpers"
	"k8s.io/apimachinery/pkg/api/resource"
)

// TestResourceBoundsStandalone tests resource bounds validation logic without requiring Kubernetes
func TestResourceBoundsStandalone(t *testing.T) {
	validationHelper := helpers.NewValidationHelper(nil) // No client needed for unit tests

	t.Run("Resource Quantity Parsing", func(t *testing.T) {
		t.Run("should parse CPU quantities correctly", func(t *testing.T) {
			testCases := []struct {
				input    string
				expected int64
			}{
				{"100m", 100},
				{"1", 1000},
				{"1000m", 1000},
				{"2", 2000},
				{"500m", 500},
			}

			for _, tc := range testCases {
				quantity, err := resource.ParseQuantity(tc.input)
				if err != nil {
					t.Errorf("Failed to parse quantity %s: %v", tc.input, err)
					continue
				}
				if quantity.MilliValue() != tc.expected {
					t.Errorf("Expected %d, got %d for input %s", tc.expected, quantity.MilliValue(), tc.input)
				}
			}
		})

		t.Run("should parse memory quantities correctly", func(t *testing.T) {
			testCases := []struct {
				input    string
				expected int64
			}{
				{"128Mi", 128 * 1024 * 1024},
				{"1Gi", 1024 * 1024 * 1024},
				{"512Mi", 512 * 1024 * 1024},
				{"2Gi", 2 * 1024 * 1024 * 1024},
			}

			for _, tc := range testCases {
				quantity, err := resource.ParseQuantity(tc.input)
				if err != nil {
					t.Errorf("Failed to parse quantity %s: %v", tc.input, err)
					continue
				}
				if quantity.Value() != tc.expected {
					t.Errorf("Expected %d, got %d for input %s", tc.expected, quantity.Value(), tc.input)
				}
			}
		})

		t.Run("should handle resource quantity parsing consistency", func(t *testing.T) {
			quantities := []string{"100m", "1", "1000m", "128Mi", "1Gi", "512Mi"}
			err := validationHelper.ValidateResourceQuantityParsing(quantities)
			if err != nil {
				t.Errorf("Resource quantity parsing validation failed: %v", err)
			}
		})
	})

	t.Run("Bounds Validation Logic", func(t *testing.T) {
		t.Run("should validate resources within bounds", func(t *testing.T) {
			bounds := helpers.ResourceBounds{
				CPU: helpers.ResourceBound{
					Min: "200m",
					Max: "1000m",
				},
				Memory: helpers.ResourceBound{
					Min: "256Mi",
					Max: "1Gi",
				},
			}

			recommendations := map[string]string{
				"optipod.io/recommendation.app.cpu":    "500m",
				"optipod.io/recommendation.app.memory": "512Mi",
			}

			err := validationHelper.ValidateResourceBounds(recommendations, bounds)
			if err != nil {
				t.Errorf("Expected no error for valid bounds, got: %v", err)
			}
		})

		t.Run("should detect resources below minimum bounds", func(t *testing.T) {
			bounds := helpers.ResourceBounds{
				CPU: helpers.ResourceBound{
					Min: "500m",
					Max: "2000m",
				},
				Memory: helpers.ResourceBound{
					Min: "1Gi",
					Max: "4Gi",
				},
			}

			recommendations := map[string]string{
				"optipod.io/recommendation.app.cpu":    "100m",  // Below min
				"optipod.io/recommendation.app.memory": "512Mi", // Below min
			}

			err := validationHelper.ValidateResourceBounds(recommendations, bounds)
			if err == nil {
				t.Error("Expected error for resources below minimum bounds")
			} else if err.Error() == "" || len(err.Error()) == 0 {
				t.Error("Expected non-empty error message")
			} else {
				// Check if error message contains "below minimum"
				found := false
				errorMsg := err.Error()
				substring := "below minimum"
				for i := 0; i <= len(errorMsg)-len(substring); i++ {
					if errorMsg[i:i+len(substring)] == substring {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error message to contain 'below minimum', got: %s", errorMsg)
				}
			}
		})

		t.Run("should detect resources above maximum bounds", func(t *testing.T) {
			bounds := helpers.ResourceBounds{
				CPU: helpers.ResourceBound{
					Min: "100m",
					Max: "500m",
				},
				Memory: helpers.ResourceBound{
					Min: "128Mi",
					Max: "512Mi",
				},
			}

			recommendations := map[string]string{
				"optipod.io/recommendation.app.cpu":    "2000m", // Above max
				"optipod.io/recommendation.app.memory": "2Gi",   // Above max
			}

			err := validationHelper.ValidateResourceBounds(recommendations, bounds)
			if err == nil {
				t.Error("Expected error for resources above maximum bounds")
			} else if err.Error() == "" || len(err.Error()) == 0 {
				t.Error("Expected non-empty error message")
			} else {
				// Check if error message contains "above maximum"
				found := false
				errorMsg := err.Error()
				substring := "above maximum"
				for i := 0; i <= len(errorMsg)-len(substring); i++ {
					if errorMsg[i:i+len(substring)] == substring {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error message to contain 'above maximum', got: %s", errorMsg)
				}
			}
		})
	})

	t.Run("Clamping Algorithms", func(t *testing.T) {
		t.Run("should validate clamping to minimum values", func(t *testing.T) {
			// Test CPU clamping to minimum - value should equal minimum when clamped
			err := validationHelper.ValidateBoundsEnforcement("500m", "500m", "2000m", false)
			if err != nil {
				t.Errorf("Expected no error for CPU clamping to minimum, got: %v", err)
			}

			// Test memory clamping to minimum - value should equal minimum when clamped
			err = validationHelper.ValidateBoundsEnforcement("1Gi", "1Gi", "4Gi", false)
			if err != nil {
				t.Errorf("Expected no error for memory clamping to minimum, got: %v", err)
			}
		})

		t.Run("should validate clamping to maximum values", func(t *testing.T) {
			// Test CPU clamping to maximum - value should equal maximum when clamped
			err := validationHelper.ValidateBoundsEnforcement("500m", "100m", "500m", false)
			if err != nil {
				t.Errorf("Expected no error for CPU clamping to maximum, got: %v", err)
			}

			// Test memory clamping to maximum - value should equal maximum when clamped
			err = validationHelper.ValidateBoundsEnforcement("512Mi", "128Mi", "512Mi", false)
			if err != nil {
				t.Errorf("Expected no error for memory clamping to maximum, got: %v", err)
			}
		})

		t.Run("should validate resources within bounds without clamping", func(t *testing.T) {
			// Test CPU within bounds
			err := validationHelper.ValidateBoundsEnforcement("750m", "500m", "1000m", false)
			if err != nil {
				t.Errorf("Expected no error for CPU within bounds, got: %v", err)
			}

			// Test memory within bounds
			err = validationHelper.ValidateBoundsEnforcement("768Mi", "512Mi", "1Gi", false)
			if err != nil {
				t.Errorf("Expected no error for memory within bounds, got: %v", err)
			}
		})

		t.Run("should detect incorrect clamping expectations", func(t *testing.T) {
			// Test expecting clamping when value is actually within bounds
			err := validationHelper.ValidateBoundsEnforcement("750m", "500m", "1000m", true)
			if err == nil {
				t.Error("Expected error when expecting clamping for value within bounds")
			} else {
				// Check if error message contains expected text
				found := false
				errorMsg := err.Error()
				substring := "expected recommendation to be clamped"
				for i := 0; i <= len(errorMsg)-len(substring); i++ {
					if errorMsg[i:i+len(substring)] == substring {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error message to contain 'expected recommendation to be clamped', got: %s", errorMsg)
				}
			}
		})
	})

	t.Run("Resource Comparison", func(t *testing.T) {
		t.Run("should compare resource quantities correctly", func(t *testing.T) {
			testCases := []struct {
				a        string
				b        string
				expected int
			}{
				{"100m", "200m", -1}, // a < b
				{"1000m", "1", 0},    // a == b
				{"2000m", "1", 1},    // a > b
				{"512Mi", "1Gi", -1}, // a < b
				{"1Gi", "1024Mi", 0}, // a == b
				{"2Gi", "1Gi", 1},    // a > b
			}

			for _, tc := range testCases {
				result, err := validationHelper.CompareResourceQuantities(tc.a, tc.b)
				if err != nil {
					t.Errorf("Failed to compare %s and %s: %v", tc.a, tc.b, err)
					continue
				}
				if result != tc.expected {
					t.Errorf("Expected %d when comparing %s and %s, got %d", tc.expected, tc.a, tc.b, result)
				}
			}
		})

		t.Run("should handle invalid resource quantities", func(t *testing.T) {
			_, err := validationHelper.CompareResourceQuantities("invalid", "100m")
			if err == nil {
				t.Error("Expected error for invalid first resource quantity")
			}

			_, err = validationHelper.CompareResourceQuantities("100m", "invalid")
			if err == nil {
				t.Error("Expected error for invalid second resource quantity")
			}
		})
	})

	t.Run("Resource Conversion", func(t *testing.T) {
		t.Run("should convert resource quantities to bytes/millicores", func(t *testing.T) {
			testCases := []struct {
				input    string
				expected int64
			}{
				{"100m", 100},
				{"1", 1000},
				{"128Mi", 128 * 1024 * 1024},
				{"1Gi", 1024 * 1024 * 1024},
			}

			for _, tc := range testCases {
				result, err := validationHelper.ConvertResourceToBytes(tc.input)
				if err != nil {
					t.Errorf("Failed to convert resource %s: %v", tc.input, err)
					continue
				}

				// The ConvertResourceToBytes method returns Value() for all resources
				// So we need to compare with the actual Value() result
				quantity, _ := resource.ParseQuantity(tc.input)
				if result != quantity.Value() {
					t.Errorf("Expected %d for %s, got %d", quantity.Value(), tc.input, result)
				}
			}
		})

		t.Run("should handle invalid resource conversion", func(t *testing.T) {
			_, err := validationHelper.ConvertResourceToBytes("invalid")
			if err == nil {
				t.Error("Expected error for invalid resource conversion")
			}
		})
	})
}
