package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Simple Unit Tests", func() {
	Context("Basic functionality", func() {
		It("should pass a simple test", func() {
			Expect(true).To(BeTrue())
		})

		It("should handle basic string operations", func() {
			testString := "optipod-test"
			Expect(testString).To(ContainSubstring("optipod"))
			Expect(testString).To(HaveLen(12))
		})

		It("should handle basic math operations", func() {
			result := 2 + 2
			Expect(result).To(Equal(4))
		})
	})
})
