# Implementation Plan: OptiPod Documentation Rebuild

## Overview

This plan outlines the tasks for building complete OptiPod documentation from scratch by analyzing the actual codebase. The documentation will be created in both Markdown (docs/) and MDX (website/) formats, following the Diátaxis framework.

## Tasks

- [x] 1. Set up documentation infrastructure
  - Create directory structure for docs/ and website/
  - Set up build tools and validation scripts
  - Configure navigation structure
  - _Requirements: 7.1, 7.4, 16.1_

- [ ] 2. Implement codebase analyzer
  - [x] 2.1 Create CRD analyzer for api/v1alpha1/
    - Parse Go source files using go/parser and go/ast
    - Extract struct definitions, field tags, and comments
    - Extract validation rules and default values
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_
  
  - [ ]* 2.2 Write property test for CRD analyzer
    - **Property 5: CRD Field Accuracy**
    - **Validates: Requirements 2.1, 2.2, 2.3, 2.5, 17.5**
  
  - [ ] 2.3 Create controller analyzer for internal/controller/
    - Extract annotation usage patterns
    - Extract operational modes and behavior
    - Extract policy selection logic
    - _Requirements: 3.1, 10.2, 10.5_
  
  - [ ] 2.4 Create webhook analyzer for internal/webhook/
    - Extract webhook configuration options
    - Extract mutation logic
    - Extract ArgoCD compatibility patterns
    - _Requirements: 10.4, 13.2_
  
  - [ ] 2.5 Create Helm chart analyzer for charts/optipod/
    - Parse values.yaml for all configuration options
    - Extract examples from examples/ directory
    - Extract RBAC manifests from templates/
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_
  
  - [ ]* 2.6 Write property test for Helm analyzer
    - **Property 6: Helm Value Accuracy**
    - **Validates: Requirements 5.1, 5.3, 5.5, 17.4**
  
  - [ ] 2.7 Create script analyzer for scripts/
    - List all script files
    - Extract usage information from comments and help text
    - Extract annotation parsing patterns from optipod-recommendation-report.sh
    - _Requirements: 4.1, 4.2, 4.3, 4.5_
  
  - [ ]* 2.8 Write property test for script analyzer
    - **Property 3: Script Reference Accuracy**
    - **Validates: Requirements 4.1, 17.1**
  
  - [ ] 2.9 Create metrics analyzer for internal/
    - Extract Prometheus metric definitions
    - Extract metric names, types, labels, and descriptions
    - Extract example PromQL queries from tests
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_
  
  - [ ]* 2.10 Write property test for metrics analyzer
    - **Property 7: Metrics Documentation Accuracy**
    - **Validates: Requirements 6.1, 6.2, 6.3, 6.4**

- [ ] 3. Checkpoint - Verify analyzers work correctly
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 4. Implement feature extractor
  - [ ] 4.1 Create feature extraction logic
    - Combine analysis results into unified feature list
    - Categorize features by type (CRD, metrics, scripts, etc.)
    - Prioritize features by user impact
    - _Requirements: 1.5_
  
  - [ ]* 4.2 Write property test for feature extraction
    - **Property 2: Feature Extraction Completeness**
    - **Validates: Requirements 2.2, 3.4, 4.1, 5.2, 6.2, 12.1, 12.2, 12.3, 12.4, 12.5**
  
  - [ ] 4.3 Categorize features by Diátaxis type
    - Identify tutorial-appropriate features
    - Identify guide-appropriate features
    - Identify reference-appropriate features
    - Identify explanation-appropriate features
    - _Requirements: 8.1, 8.2, 8.3, 8.4_

- [ ] 5. Implement content generators
  - [ ] 5.1 Create tutorial content generator
    - Generate step-by-step learning content
    - Include hands-on examples
    - Focus on understanding concepts
    - _Requirements: 8.1, 9.1, 9.2, 9.3_
  
  - [ ] 5.2 Create guide content generator
    - Generate task-oriented how-to content
    - Include specific procedures
    - Focus on accomplishing goals
    - _Requirements: 8.2, 11.1, 11.2, 11.3, 11.4, 11.5_
  
  - [ ] 5.3 Create reference content generator
    - Generate comprehensive API documentation
    - Include all fields, options, and parameters
    - Focus on completeness and accuracy
    - _Requirements: 8.3, 12.1, 12.2, 12.3, 12.4, 12.5_
  
  - [ ] 5.4 Create explanation content generator
    - Generate conceptual understanding content
    - Include architecture diagrams
    - Focus on the "why" and "how it works"
    - _Requirements: 8.4, 10.1, 10.2, 10.3, 10.4, 10.5_
  
  - [ ] 5.5 Create Mermaid diagram generator
    - Generate architecture diagrams
    - Generate workflow flowcharts
    - Generate policy selection diagrams
    - _Requirements: 14.1, 14.2, 14.5_
  
  - [ ]* 5.6 Write property test for diagram accessibility
    - **Property 15: Diagram Accessibility**
    - **Validates: Requirements 14.4**

- [ ] 6. Checkpoint - Verify content generators work
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Generate Getting Started documentation
  - [ ] 7.1 Generate installation guide
    - Extract installation methods from Helm chart
    - Include prerequisites and verification steps
    - Add troubleshooting tips from tests
    - Create both docs/getting-started/installation.md and website version
    - _Requirements: 9.1, 9.4_
  
  - [ ] 7.2 Generate quick start guide
    - Use examples from config/samples/
    - Include step-by-step instructions
    - Add verification commands
    - Create both docs/getting-started/quick-start.md and website version
    - _Requirements: 9.2, 9.4_
  
  - [ ] 7.3 Generate first policy guide
    - Extract policy examples from config/samples/
    - Include explanation of each field
    - Add verification steps
    - Create both docs/getting-started/first-policy.md and website version
    - _Requirements: 9.3, 9.4_
  
  - [ ]* 7.4 Write property test for dual documentation sync
    - **Property 8: Dual Documentation Synchronization**
    - **Validates: Requirements 7.1, 7.2, 7.5**

- [ ] 8. Generate Concepts documentation
  - [ ] 8.1 Generate architecture documentation
    - Extract architecture from code structure
    - Create architecture diagram using Mermaid
    - Explain controller and webhook components
    - Create both docs/concepts/architecture.md and website version
    - _Requirements: 10.1, 14.1_
  
  - [ ] 8.2 Generate operational modes documentation
    - Extract modes from api/v1alpha1/ types
    - Explain Auto, Recommend, and Disabled modes
    - Include mode transition examples
    - Create both docs/concepts/modes.md and website version
    - _Requirements: 10.2_
  
  - [ ] 8.3 Generate safety model documentation
    - Extract safety checks from internal/safety/ code
    - Explain memory safety and bounds enforcement
    - Include safety factor explanation
    - Create both docs/concepts/safety-model.md and website version
    - _Requirements: 10.3_
  
  - [x] 8.4 Generate update strategies documentation
    - Extract SSA and webhook strategies from code
    - Create workflow diagrams using Mermaid
    - Explain when to use each strategy
    - Create both docs/concepts/update-strategies.md and website version
    - _Requirements: 10.4, 14.2_
  
  - [ ]* 8.5 Write property test for documentation-code alignment
    - **Property 13: Documentation-Code Alignment**
    - **Validates: Requirements 10.1, 10.2, 10.3, 10.4, 10.5, 13.1, 13.2, 13.3, 13.4, 13.5**

- [ ] 9. Checkpoint - Verify Getting Started and Concepts docs
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 10. Generate Guides documentation
  - [x] 10.1 Generate creating policies guide
    - Use CRD spec and examples from config/samples/
    - Include field-by-field explanation
    - Add validation and troubleshooting tips
    - Create both docs/guides/creating-policies.md and website version
    - _Requirements: 11.1_
  
  - [x] 10.2 Generate reviewing recommendations guide
    - Extract annotation format from controller and script code
    - Document optipod-recommendation-report.sh usage
    - Include example output and interpretation
    - Create both docs/guides/reviewing-recs.md and website version
    - _Requirements: 11.2_
  
  - [ ]* 10.3 Write property test for annotation format consistency
    - **Property 4: Annotation Format Consistency**
    - **Validates: Requirements 3.2, 3.3, 17.2**
  
  - [x] 10.4 Generate switching to Auto mode guide
    - Extract mode transition logic from code
    - Include safety considerations
    - Add rollback procedures
    - Create both docs/guides/switching-to-auto.md and website version
    - _Requirements: 11.3_
  
  - [x] 10.5 Generate troubleshooting guide
    - Extract common issues from test scenarios
    - Extract error handling from code
    - Include diagnostic commands
    - Create both docs/guides/troubleshooting.md and website version
    - _Requirements: 11.4, 9.5_
  
  - [x] 10.6 Generate GitOps integration guide
    - Extract webhook ArgoCD compatibility patterns
    - Include example configurations
    - Add verification steps
    - Create both docs/guides/gitops-integration.md and website version
    - _Requirements: 11.5_

- [ ] 11. Generate Reference documentation
  - [x] 11.1 Generate CRD specification reference
    - Use extracted CRD spec from analyzer
    - Document all fields with types and validation
    - Include default values and examples
    - Create both docs/reference/crd-spec.md and website version
    - _Requirements: 12.1_
  
  - [x] 11.2 Generate annotations reference
    - Use extracted annotation formats from analyzer
    - Document all annotation types
    - Include examples from actual usage
    - Create both docs/reference/annotations.md and website version
    - _Requirements: 12.2_
  
  - [x] 11.3 Generate metrics reference
    - Use extracted metrics from analyzer
    - Document all metrics with types and labels
    - Include example PromQL queries
    - Create both docs/reference/metrics.md and website version
    - _Requirements: 12.3_
  
  - [x] 11.4 Generate CLI tools reference
    - Use extracted script information from analyzer
    - Document all scripts with usage and parameters
    - Include correct download URLs
    - Create both docs/reference/cli-tools.md and website version
    - _Requirements: 12.4_
  
  - [ ]* 11.5 Write property test for URL construction
    - **Property 11: URL Construction Correctness**
    - **Validates: Requirements 4.4, 17.3**
  
  - [x] 11.6 Generate Helm values reference
    - Use extracted Helm values from analyzer
    - Document all configuration options
    - Include default values and examples
    - Create both docs/reference/helm-values.md and website version
    - _Requirements: 12.5_

- [ ] 12. Checkpoint - Verify Guides and Reference docs
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 13. Generate Advanced Topics documentation
  - [x] 13.1 Generate RBAC configuration guide
    - Extract RBAC from config/rbac/ manifests
    - Document required permissions
    - Include least-privilege examples
    - Create both docs/advanced/rbac.md and website version
    - _Requirements: 13.1_
  
  - [x] 13.2 Generate webhook configuration guide
    - Extract webhook config from internal/webhook/ code
    - Document TLS setup and cert-manager integration
    - Include HA and performance tuning
    - Create both docs/advanced/webhook-config.md and website version
    - _Requirements: 13.2_
  
  - [ ] 13.3 Generate operations guide
    - Extract operational procedures from controller behavior
    - Include health checks and monitoring
    - Add incident response procedures
    - Create both docs/advanced/operations.md and website version
    - _Requirements: 13.3_
  
  - [ ] 13.4 Generate observability guide
    - Extract metrics and logging from code
    - Include Grafana dashboard setup
    - Add alerting rules
    - Create both docs/advanced/observability.md and website version
    - _Requirements: 13.4_
  
  - [ ] 13.5 Generate performance tuning guide
    - Extract configuration options from code
    - Include scaling recommendations
    - Add resource requirements
    - Create both docs/advanced/performance.md and website version
    - _Requirements: 13.5_

- [ ] 14. Implement validation system
  - [ ] 14.1 Create script reference validator
    - Check all script references against scripts/ directory
    - Report missing scripts
    - _Requirements: 17.1_
  
  - [ ] 14.2 Create annotation format validator
    - Check annotation formats against controller code
    - Report format mismatches
    - _Requirements: 17.2_
  
  - [ ] 14.3 Create URL validator
    - Check all URLs are correctly constructed
    - Verify URLs point to existing files
    - _Requirements: 17.3_
  
  - [ ] 14.4 Create Helm value validator
    - Check documented values exist in values.yaml
    - Verify default values match
    - _Requirements: 17.4_
  
  - [ ] 14.5 Create CRD field validator
    - Check documented fields exist in types.go
    - Verify types and validation rules match
    - _Requirements: 17.5_
  
  - [ ] 14.6 Create link validator
    - Check all internal links resolve correctly
    - Verify links work in both docs/ and website/
    - _Requirements: 7.5, 16.3_
  
  - [ ]* 14.7 Write property test for internal link validity
    - **Property 12: Internal Link Validity**
    - **Validates: Requirements 7.5, 16.3**
  
  - [ ] 14.8 Create code example validator
    - Verify kubectl command syntax
    - Verify Helm command syntax
    - Verify bash script syntax
    - _Requirements: 15.2, 15.3, 15.5_
  
  - [ ]* 14.9 Write property test for code example executability
    - **Property 10: Code Example Executability**
    - **Validates: Requirements 15.2, 15.3, 15.5**

- [ ] 15. Implement navigation and structure
  - [ ] 15.1 Create website navigation configuration
    - Define navigation hierarchy
    - Add all documentation pages
    - Ensure logical grouping
    - _Requirements: 16.1_
  
  - [ ] 15.2 Add table of contents to long documents
    - Identify documents > 500 lines
    - Generate TOC for each
    - Add TOC to both Markdown and MDX versions
    - _Requirements: 16.2_
  
  - [ ]* 15.3 Write property test for long document navigation
    - **Property 16: Long Document Navigation**
    - **Validates: Requirements 16.2**
  
  - [ ] 15.4 Add cross-references between documents
    - Identify related documents
    - Add "See also" sections
    - Verify links work
    - _Requirements: 16.3_
  
  - [ ] 15.5 Create documentation index
    - Generate sitemap of all documentation
    - Create index page with all topics
    - Add to both docs/ and website/
    - _Requirements: 16.5_

- [ ] 16. Final validation and polish
  - [ ] 16.1 Run all validation checks
    - Execute all validators
    - Fix any errors found
    - Verify all properties pass
    - _Requirements: 17.1, 17.2, 17.3, 17.4, 17.5_
  
  - [ ] 16.2 Test all code examples
    - Execute kubectl commands
    - Execute Helm commands
    - Execute bash scripts
    - Verify all examples work
    - _Requirements: 15.1, 15.2, 15.3, 15.4, 15.5_
  
  - [ ]* 16.3 Write property test for code example source accuracy
    - **Property 9: Code Example Source Accuracy**
    - **Validates: Requirements 3.5, 4.5, 14.3, 15.1, 15.4**
  
  - [ ] 16.4 Verify dual documentation synchronization
    - Compare docs/ and website/ content
    - Ensure examples match
    - Verify navigation matches
    - _Requirements: 7.1, 7.2, 7.4, 7.5_
  
  - [ ] 16.5 Test website build
    - Build website locally
    - Verify all pages render correctly
    - Test navigation
    - Check responsive design
    - _Requirements: 7.1, 16.1_
  
  - [ ] 16.6 Add accessibility features
    - Verify all diagrams have alt text
    - Check color contrast
    - Test keyboard navigation
    - _Requirements: 14.4_

- [ ] 17. Final checkpoint - Documentation complete
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional property-based tests and can be skipped for faster completion
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties
- All documentation must be created in both docs/ (Markdown) and website/ (MDX) formats
- All code examples must be derived from actual codebase files
- All validation checks must pass before documentation is considered complete
