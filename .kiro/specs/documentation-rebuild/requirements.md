# Requirements Document: OptiPod Documentation Rebuild

## Introduction

This specification defines the requirements for building complete OptiPod documentation from scratch. The documentation will be created by analyzing the actual codebase, tests, configuration files, and scripts - not by referencing any existing documentation. This ensures accuracy and alignment with the actual implementation.

## Glossary

- **OptiPod**: Kubernetes operator that provides explainable recommendations for CPU and memory requests/limits
- **Diátaxis**: Documentation framework with four types: tutorials, how-to guides, reference, explanation
- **GitOps**: Declarative infrastructure and application management using Git as source of truth
- **SSA**: Server-Side Apply - Kubernetes feature for managing field ownership
- **Webhook**: Kubernetes admission webhook that mutates pods at creation time
- **CRD**: Custom Resource Definition - Kubernetes extension mechanism
- **OptimizationPolicy**: The CRD that defines optimization behavior for workloads
- **Prometheus**: Time-series database and monitoring system
- **metrics-server**: Kubernetes component that provides resource metrics
- **ArgoCD**: GitOps continuous delivery tool for Kubernetes
- **Helm**: Package manager for Kubernetes
- **MDX**: Markdown with JSX - format used in the website documentation
- **Mermaid**: Diagram and flowchart tool that uses text-based syntax

## Requirements

### Requirement 1: Codebase Feature Discovery

**User Story:** As a documentation writer, I want to discover features from the actual codebase, so that documentation is based on reality.

#### Acceptance Criteria

1. WHEN discovering features, THE System SHALL analyze source code in api/, internal/, cmd/ directories
2. WHEN discovering features, THE System SHALL analyze test files to understand behavior and usage
3. WHEN discovering features, THE System SHALL analyze Helm charts in charts/optipod/ directory
4. WHEN discovering features, THE System SHALL analyze actual scripts in scripts/ directory
5. THE System SHALL create a feature list based solely on code analysis

### Requirement 2: CRD Specification Extraction

**User Story:** As a documentation writer, I want to extract CRD specifications from code, so that reference documentation is accurate.

#### Acceptance Criteria

1. WHEN extracting CRD specs, THE System SHALL analyze api/v1alpha1/optimizationpolicy_types.go
2. THE System SHALL extract all field definitions, types, and validation rules
3. THE System SHALL extract default values from code
4. THE System SHALL extract field descriptions from code comments
5. THE System SHALL document required vs optional fields

### Requirement 3: Annotation Format Extraction

**User Story:** As a documentation writer, I want to extract annotation formats from code, so that examples are correct.

#### Acceptance Criteria

1. WHEN documenting annotations, THE System SHALL analyze internal/controller/ code for annotation usage
2. THE System SHALL analyze scripts/optipod-recommendation-report.sh for annotation parsing
3. THE System SHALL extract the exact annotation key format from code
4. THE System SHALL document all annotation types (recommendation, policy, status)
5. THE System SHALL provide examples based on actual code usage

### Requirement 4: Script Documentation from Source

**User Story:** As a user, I want documentation for available scripts, so that I know what tools are available.

#### Acceptance Criteria

1. WHEN documenting scripts, THE System SHALL list all scripts in scripts/ directory
2. THE System SHALL extract script purpose from comments and code
3. THE System SHALL document script usage and parameters
4. THE System SHALL provide correct download URLs based on actual file paths
5. THE System SHALL include example usage from script help text or code

### Requirement 5: Helm Chart Configuration Documentation

**User Story:** As a user, I want complete Helm chart documentation, so that I can configure OptiPod correctly.

#### Acceptance Criteria

1. WHEN documenting Helm values, THE System SHALL analyze charts/optipod/values.yaml
2. THE System SHALL extract all configuration options with descriptions
3. THE System SHALL document default values from the actual values.yaml file
4. THE System SHALL extract examples from charts/optipod/examples/ directory
5. THE System SHALL document required vs optional values

### Requirement 6: Metrics Documentation from Code

**User Story:** As a user, I want documentation of available metrics, so that I can monitor OptiPod.

#### Acceptance Criteria

1. WHEN documenting metrics, THE System SHALL analyze internal/controller/ and internal/webhook/ code
2. THE System SHALL extract all Prometheus metric definitions
3. THE System SHALL document metric names, types, and labels
4. THE System SHALL extract metric descriptions from code comments
5. THE System SHALL provide example PromQL queries based on actual usage

### Requirement 7: Dual Documentation Creation

**User Story:** As a documentation maintainer, I want documentation in both docs/ and website/ folders, so that users can access it in their preferred format.

#### Acceptance Criteria

1. WHEN creating documentation, THE System SHALL create both Markdown (docs/) and MDX (website/) versions
2. THE System SHALL ensure content is synchronized between both versions
3. THE System SHALL adapt formatting appropriately for each medium
4. THE System SHALL maintain consistent navigation structure in both locations
5. THE System SHALL verify all internal links work in both versions

### Requirement 8: Diátaxis Framework Implementation

**User Story:** As a user, I want documentation organized by the Diátaxis framework, so that I can find the right type of information for my needs.

#### Acceptance Criteria

1. THE System SHALL create tutorial documentation for learning-oriented content
2. THE System SHALL create how-to guides for task-oriented content
3. THE System SHALL create reference documentation for information-oriented content
4. THE System SHALL create explanation documentation for understanding-oriented content
5. THE System SHALL clearly label each document with its Diátaxis category

### Requirement 9: Getting Started Documentation

**User Story:** As a new user, I want clear getting started documentation, so that I can quickly install and use OptiPod.

#### Acceptance Criteria

1. THE System SHALL create installation documentation based on Helm chart and README
2. THE System SHALL create a quick start guide with examples from config/samples/
3. THE System SHALL create first policy documentation with step-by-step instructions
4. THE System SHALL include verification steps based on actual controller behavior
5. THE System SHALL provide troubleshooting tips based on test scenarios

### Requirement 10: Architecture and Concepts Documentation

**User Story:** As a user, I want to understand OptiPod architecture and concepts, so that I can use it effectively.

#### Acceptance Criteria

1. THE System SHALL document architecture based on code structure in cmd/, internal/
2. THE System SHALL explain operational modes based on api/v1alpha1/ types
3. THE System SHALL document safety model based on internal/safety/ code
4. THE System SHALL explain update strategies based on internal/controller/ and internal/webhook/
5. THE System SHALL document policy selection based on internal/policy/ code

### Requirement 11: Task-Oriented Guides

**User Story:** As a user, I want task-oriented guides, so that I can accomplish specific goals.

#### Acceptance Criteria

1. THE System SHALL create a guide for creating policies based on CRD spec and examples
2. THE System SHALL create a guide for reviewing recommendations based on annotation format
3. THE System SHALL create a guide for switching to Auto mode based on mode transitions in code
4. THE System SHALL create a troubleshooting guide based on test scenarios and error handling
5. THE System SHALL create a guide for GitOps integration based on webhook implementation

### Requirement 12: Complete Reference Documentation

**User Story:** As a user, I want complete reference documentation, so that I can look up specific details.

#### Acceptance Criteria

1. THE System SHALL document the complete CRD specification from api/v1alpha1/
2. THE System SHALL document all annotation formats from controller and script code
3. THE System SHALL document all Prometheus metrics from internal/ code
4. THE System SHALL document all CLI tools and scripts from scripts/ directory
5. THE System SHALL document all Helm chart values from charts/optipod/values.yaml

### Requirement 13: Advanced Topics Documentation

**User Story:** As an advanced user, I want documentation for advanced features, so that I can use OptiPod in complex scenarios.

#### Acceptance Criteria

1. THE System SHALL document RBAC configuration from config/rbac/ manifests
2. THE System SHALL document webhook configuration from internal/webhook/ code
3. THE System SHALL document operational procedures based on controller behavior
4. THE System SHALL document observability setup based on metrics and logging code
5. THE System SHALL document performance tuning based on configuration options

### Requirement 14: Visual Aids and Diagrams

**User Story:** As a user, I want diagrams and visual aids, so that I can understand complex concepts more easily.

#### Acceptance Criteria

1. WHEN explaining architecture, THE System SHALL create architecture diagrams using Mermaid
2. WHEN explaining workflows, THE System SHALL create flowcharts using Mermaid
3. WHEN showing examples, THE System SHALL include code snippets from actual files
4. THE System SHALL ensure diagrams are accessible and have descriptions
5. THE System SHALL create diagrams based on code structure and flow

### Requirement 15: Code Example Accuracy

**User Story:** As a user, I want all code examples to work, so that I can copy-paste them successfully.

#### Acceptance Criteria

1. WHEN providing policy examples, THE System SHALL use examples from config/samples/
2. WHEN showing kubectl commands, THE System SHALL verify syntax against Kubernetes API
3. WHEN showing Helm commands, THE System SHALL verify against actual chart structure
4. WHEN showing script usage, THE System SHALL extract from actual script files
5. THE System SHALL ensure all examples are complete and executable

### Requirement 16: Navigation and Discoverability

**User Story:** As a user, I want easy navigation, so that I can find the information I need quickly.

#### Acceptance Criteria

1. THE System SHALL create a clear navigation structure for the website
2. THE System SHALL include a table of contents in long documents
3. THE System SHALL provide cross-references between related documents
4. THE System SHALL include a search-friendly structure
5. THE System SHALL create an index or sitemap of all documentation

### Requirement 17: Testing and Validation

**User Story:** As a documentation maintainer, I want to validate documentation, so that it remains accurate.

#### Acceptance Criteria

1. THE System SHALL verify all script references point to actual files in scripts/
2. THE System SHALL verify all annotation formats match controller code
3. THE System SHALL verify all URLs are constructed correctly from file paths
4. THE System SHALL verify all Helm values exist in values.yaml
5. THE System SHALL verify all CRD fields exist in types.go
