package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestE2EUnit(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Unit Test Suite")
}
