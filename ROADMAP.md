# OptiPod Roadmap

This document outlines the current implementation status and future plans for OptiPod features.

## 🎯 Current Status

OptiPod is in **active development** with core functionality implemented and tested. The project follows a spec-driven development approach with comprehensive testing.

## ✅ Implemented Features

### Core Functionality
- **Automatic Resource Optimization**: ✅ Fully implemented
  - CPU and memory request optimization based on actual usage
  - Percentile-based recommendations (P50, P90, P99)
  - Configurable safety factors and rolling windows

- **Multiple Operational Modes**: ✅ Fully implemented
  - Auto mode (automatic application)
  - Recommend mode (review before applying)
  - Disabled mode (preserve historical data)

- **Server-Side Apply (SSA)**: ✅ Fully implemented
  - Field-level ownership tracking
  - GitOps compatibility (ArgoCD, Flux)
  - Conflict resolution and audit trails

- **Safety-First Approach**: ✅ Fully implemented
  - Resource bounds enforcement (min/max limits)
  - Memory decrease safety thresholds
  - In-place resize support (Kubernetes 1.29+)
  - Graceful fallback for pod recreation scenarios

### Workload Management
- **Multi-Workload Support**: ✅ Fully implemented
  - Deployments, StatefulSets, DaemonSets
  - Multi-container workload optimization
  - Workload type filtering (include/exclude)

- **Multi-Tenant Ready**: ✅ Fully implemented
  - Namespace-based selection with allow/deny lists
  - Label-based workload selection
  - Policy weight-based prioritization

### Observability & Testing
- **Comprehensive Observability**: ✅ Fully implemented
  - Prometheus metrics exposure
  - Kubernetes events generation
  - Detailed status reporting in policy CRDs

- **Property-Based Testing**: ✅ Fully implemented
  - Extensive test coverage with gopter
  - Correctness properties validation
  - E2E test suite with real Kubernetes clusters

### CI/CD & Release
- **Production-Ready CI/CD**: ✅ Fully implemented
  - Multi-architecture builds (amd64, arm64, s390x, ppc64le)
  - Security scanning with Trivy
  - Image signing with cosign
  - SBOM generation and provenance attestations

## 🚧 Work in Progress

### Metrics Providers
- **Kubernetes metrics-server**: ✅ Basic implementation complete
  - ⚠️ Limited testing coverage
  - 🔄 Enhanced integration testing in progress

- **Prometheus Integration**: 🚧 Partial implementation
  - ✅ Basic Prometheus query support
  - 🔄 Advanced query optimization in progress
  - 🔄 High availability Prometheus setup testing
  - 🔄 Custom metrics and alerting integration

### Advanced Features
- **Custom Metrics Providers**: 🚧 Framework in progress
  - ✅ Plugin architecture designed
  - 🔄 Implementation of custom provider interface
  - 🔄 Documentation and examples

- **Enhanced ArgoCD Integration**: 🚧 In progress
  - ✅ Basic SSA compatibility implemented
  - 🔄 Advanced sync conflict resolution
  - 🔄 ArgoCD Application health integration
  - 🔄 GitOps workflow best practices documentation

## 📋 Planned Features

### Q1 2025
- **Performance & Scale Enhancements**
  - Cluster-wide performance benchmarks
  - Large-scale deployment testing (1000+ workloads)
  - Memory and CPU optimization for the controller itself

- **Advanced Metrics & Analytics**
  - Cost savings reporting and dashboards
  - Historical trend analysis
  - Recommendation confidence scoring

### Q2 2025
- **Enterprise Features**
  - Multi-cluster support and management
  - Advanced RBAC with fine-grained permissions
  - Audit logging and compliance reporting

- **Integration Ecosystem**
  - Helm chart for easier deployment
  - Operator Lifecycle Manager (OLM) support
  - Integration with popular monitoring stacks (Grafana, Datadog)

### Q3 2025
- **Machine Learning Enhancements**
  - Predictive scaling based on historical patterns
  - Anomaly detection for resource usage
  - Intelligent workload classification

- **Advanced Optimization Strategies**
  - Node-aware optimization (considering node capacity)
  - Cost-aware optimization (cloud provider pricing)
  - Performance-aware optimization (SLA considerations)

### Future Considerations
- **Multi-Cloud Support**
  - Cloud provider-specific optimizations
  - Cross-cloud cost comparison
  - Cloud-native service integration

- **Developer Experience**
  - CLI tool for policy management
  - Web UI for visualization and management
  - IDE extensions for policy development

## 🔄 Migration Path

### From Current State
If you're using OptiPod today, you have access to all **Implemented Features**. The system is production-ready for:
- Basic resource optimization
- GitOps workflows with ArgoCD
- Multi-tenant Kubernetes clusters
- CI/CD integration

### Upgrading Strategy
- **Backward Compatibility**: All new features maintain backward compatibility
- **Gradual Adoption**: New features can be adopted incrementally
- **Migration Guides**: Detailed guides provided for major version updates

## 🤝 Contributing

We welcome contributions to accelerate the roadmap! Priority areas:

1. **Testing**: Help expand E2E test coverage for work-in-progress features
2. **Documentation**: Improve examples and use case documentation
3. **Integration**: Build integrations with popular tools and platforms
4. **Performance**: Contribute benchmarks and optimization improvements

See [CONTRIBUTING.md](docs/CONTRIBUTING.md) for detailed contribution guidelines.

## 📞 Feedback

Have suggestions for the roadmap? We'd love to hear from you:
- [GitHub Issues](https://github.com/Sagart-cactus/optipod/issues) for feature requests
- [GitHub Discussions](https://github.com/Sagart-cactus/optipod/discussions) for general feedback
- [Community Slack](#) for real-time discussions (coming soon)

---

**Last Updated**: December 2024  
**Next Review**: March 2025

> This roadmap is a living document and may be updated based on community feedback, technical discoveries, and changing requirements.