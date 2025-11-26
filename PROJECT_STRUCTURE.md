# OptiPod Project Structure

This document describes the initial project structure created for the OptiPod Kubernetes operator.

## Overview

OptiPod is a Kubernetes operator built with Kubebuilder that automatically optimizes workload resource requests based on actual usage patterns.

## Technology Stack

- **Go Version**: 1.25.4
- **Kubebuilder Version**: 4.10.1
- **Kubernetes Version**: 1.34.1
- **Controller Runtime**: v0.22.4
- **Property-Based Testing**: gopter v0.2.11

## Project Structure

```
.
├── api/                          # API definitions
│   └── v1alpha1/                 # v1alpha1 API version
│       ├── groupversion_info.go  # API group and version info
│       ├── optimizationpolicy_types.go  # OptimizationPolicy CRD types
│       └── zz_generated.deepcopy.go     # Generated deep copy methods
│
├── cmd/                          # Main application entry point
│   └── main.go                   # Operator main function
│
├── config/                       # Kubernetes manifests
│   ├── crd/                      # CRD manifests
│   │   └── bases/                # Base CRD definitions
│   │       └── optipod.optipod.io_optimizationpolicies.yaml
│   ├── default/                  # Default kustomization
│   ├── manager/                  # Manager deployment
│   ├── prometheus/               # Prometheus monitoring
│   ├── rbac/                     # RBAC permissions
│   └── samples/                  # Sample resources
│       └── optipod_v1alpha1_optimizationpolicy.yaml
│
├── internal/                     # Internal packages
│   └── controller/               # Controllers
│       ├── optimizationpolicy_controller.go      # Main controller
│       ├── optimizationpolicy_controller_test.go # Controller tests
│       ├── gopter_test.go        # Gopter setup verification
│       └── suite_test.go         # Test suite setup
│
├── test/                         # Test utilities
│   ├── e2e/                      # End-to-end tests
│   └── utils/                    # Test utilities
│
├── hack/                         # Build scripts
│   └── boilerplate.go.txt        # License header template
│
├── bin/                          # Built binaries
│   ├── manager                   # Operator binary
│   ├── controller-gen            # Code generator
│   └── setup-envtest             # Test environment setup
│
├── go.mod                        # Go module definition
├── go.sum                        # Go module checksums
├── Makefile                      # Build automation
├── Dockerfile                    # Container image definition
└── PROJECT                       # Kubebuilder project metadata
```

## Key Components

### API Group and Version

- **Domain**: `optipod.io`
- **Group**: `optipod`
- **Version**: `v1alpha1`
- **Kind**: `OptimizationPolicy`
- **Full API Group**: `optipod.optipod.io/v1alpha1`

### Custom Resource Definition (CRD)

The `OptimizationPolicy` CRD is defined in `api/v1alpha1/optimizationpolicy_types.go` with:
- **Spec**: Defines desired state (to be implemented in future tasks)
- **Status**: Defines observed state with conditions support
- **Subresource**: Status subresource enabled

### Controller

The `OptimizationPolicyReconciler` in `internal/controller/optimizationpolicy_controller.go`:
- Watches `OptimizationPolicy` resources
- Reconciles desired state with actual state
- Has RBAC permissions for managing OptimizationPolicy resources

### Dependencies

Core dependencies installed:
- `sigs.k8s.io/controller-runtime` - Controller framework
- `k8s.io/client-go` - Kubernetes client
- `k8s.io/apimachinery` - Kubernetes API machinery
- `github.com/leanovate/gopter` - Property-based testing
- `github.com/onsi/ginkgo/v2` - BDD testing framework
- `github.com/onsi/gomega` - Matcher library

## Build and Test Commands

### Build the operator
```bash
make build
```

### Run tests
```bash
make test
# or
go test ./...
```

### Generate CRD manifests
```bash
make manifests
```

### Install CRDs into cluster
```bash
make install
```

### Run the operator locally
```bash
make run
```

### Build and push Docker image
```bash
make docker-build docker-push IMG=<registry>/optipod:tag
```

## Next Steps

The following tasks from the implementation plan will build upon this foundation:

1. **Task 2**: Define CRD and core data models
2. **Task 3**: Implement metrics provider interface
3. **Task 4**: Implement recommendation engine
4. **Task 5**: Implement application engine
5. And so on...

## Verification

The project has been verified to:
- ✅ Build successfully (`make build`)
- ✅ Pass all tests (`go test ./...`)
- ✅ Generate CRD manifests correctly
- ✅ Have gopter property-based testing framework installed and working
- ✅ Follow Kubernetes operator conventions
- ✅ Use the correct API group `optipod.io`

## References

- [Kubebuilder Book](https://book.kubebuilder.io/)
- [Controller Runtime](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [Gopter Documentation](https://github.com/leanovate/gopter)
