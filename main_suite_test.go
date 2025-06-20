package bosh_windows_acceptance_tests_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBoshWindowsAcceptanceTests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "WindowsStemcellAcceptanceTests Suite")
}
