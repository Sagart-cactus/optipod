package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Resource Bounds Unit Tests", func() {
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
				quantityA, err := resource.ParseQuantity(tc.a)
				Expect(err).NotTo(HaveOccurred())
				quantityB, err := resource.ParseQuantity(tc.b)
				Expect(err).NotTo(HaveOccurred())

				result := quantityA.Cmp(quantityB)
				Expect(result).To(Equal(tc.expected))
			}
		})

		It("should handle invalid resource quantities", func() {
			_, err := resource.ParseQuantity("invalid")
			Expect(err).To(HaveOccurred())
		})
	})
})