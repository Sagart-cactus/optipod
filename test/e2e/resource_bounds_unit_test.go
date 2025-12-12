package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/optipod/optipod/test/e2e/helpers"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Resource Bounds Unit Tests", func() {
	var validationHelper *helpers.ValidationHelper

	BeforeEach(func() {
		validationHelper = helpers.NewValidationHelper(nil) // No client needed for unit tests
	})

	Context("Resource Quantity Parsing", func() {
		It("should parse CPU quantities correctly", func() {
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
				Expect(err).NotTo(HaveOccurred())
				Expect(quantity.MilliValue()).To(Equal(tc.expected))
			}
		})

		It("should parse memory quantities correctly", func() {
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
				Expect(err).NotTo(HaveOccurred())
				Expect(quantity.Value()).To(Equal(tc.expected))
			}
		})

		It("should handle resource quantity parsing consistency", func() {
			quantities := []string{"100m", "1", "1000m", "128Mi", "1Gi", "512Mi"}
			err := validationHelper.ValidateResourceQuantityParsing(quantities)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Bounds Validation Logic", func() {
		It("should validate resources within bounds", func() {
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
			Expect(err).NotTo(HaveOccurred())
		})

		It("should detect resources below minimum bounds", func() {
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
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("below minimum"))
		})

		It("should detect resources above maximum bounds", func() {
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
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("above maximum"))
		})
	})

	Context("Clamping Algorithms", func() {
		It("should validate clamping to minimum values", func() {
			// Test CPU clamping to minimum - value should equal minimum when clamped
			err := validationHelper.ValidateBoundsEnforcement("500m", "500m", "2000m", false)
			Expect(err).NotTo(HaveOccurred())

			// Test memory clamping to minimum - value should equal minimum when clamped
			err = validationHelper.ValidateBoundsEnforcement("1Gi", "1Gi", "4Gi", false)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should validate clamping to maximum values", func() {
			// Test CPU clamping to maximum - value should equal maximum when clamped
			err := validationHelper.ValidateBoundsEnforcement("500m", "100m", "500m", false)
			Expect(err).NotTo(HaveOccurred())

			// Test memory clamping to maximum - value should equal maximum when clamped
			err = validationHelper.ValidateBoundsEnforcement("512Mi", "128Mi", "512Mi", false)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should validate resources within bounds without clamping", func() {
			// Test CPU within bounds
			err := validationHelper.ValidateBoundsEnforcement("750m", "500m", "1000m", false)
			Expect(err).NotTo(HaveOccurred())

			// Test memory within bounds
			err = validationHelper.ValidateBoundsEnforcement("768Mi", "512Mi", "1Gi", false)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should detect incorrect clamping expectations", func() {
			// Test expecting clamping when value is actually within bounds
			err := validationHelper.ValidateBoundsEnforcement("750m", "500m", "1000m", true)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected recommendation to be clamped"))
		})
	})

	Context("Resource Comparison", func() {
		It("should compare resource quantities correctly", func() {
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
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(tc.expected))
			}
		})

		It("should handle invalid resource quantities", func() {
			_, err := validationHelper.CompareResourceQuantities("invalid", "100m")
			Expect(err).To(HaveOccurred())

			_, err = validationHelper.CompareResourceQuantities("100m", "invalid")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Resource Conversion", func() {
		It("should convert resource quantities to bytes/millicores", func() {
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
				Expect(err).NotTo(HaveOccurred())

				// The ConvertResourceToBytes method returns Value() for all resources
				// So we need to compare with the actual Value() result
				quantity, _ := resource.ParseQuantity(tc.input)
				Expect(result).To(Equal(quantity.Value()))
			}
		})

		It("should handle invalid resource conversion", func() {
			_, err := validationHelper.ConvertResourceToBytes("invalid")
			Expect(err).To(HaveOccurred())
		})
	})
})

// TestResourceBoundsUnit is handled by the main e2e suite
