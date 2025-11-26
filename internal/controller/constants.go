package controller

// Status constants
const (
	StatusSkipped     = "Skipped"
	StatusError       = "Error"
	StatusRecommended = "Recommended"
	StatusApplied     = "Applied"
)

// Workload kind constants
const (
	KindDeployment  = "Deployment"
	KindStatefulSet = "StatefulSet"
	KindDaemonSet   = "DaemonSet"
)

// Condition type constants
const (
	ConditionTypeReady = "Ready"
)

// Test constants
const (
	TestWorkloadName  = "test-workload"
	TestNamespace     = "default"
	TestContainerName = "test-container"
	TestPodName       = "test-pod"
)
