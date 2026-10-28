# OptiPod Governance

OptiPod is an open-source project maintained in the open. This document describes how decisions are made and how the project is run.

## Project Scope

OptiPod is a Kubernetes operator focused on safe, explainable optimization of CPU/memory requests and limits for Kubernetes workloads, with GitOps-friendly apply semantics.

## Maintainer Model

OptiPod currently uses a **single-maintainer** model.

- **Project Lead / Maintainer**: Repo owner (see `CODEOWNERS` if/when added)
- **Contributors**: Anyone who submits issues, discussions, documentation, or pull requests

## Decision Making

- The Maintainer makes final decisions to keep the project moving.
- When possible, decisions are made by consensus in public (issues/discussions/PR threads).
- Significant changes (APIs/CRDs, defaults, safety behavior, release process) should be proposed via an issue or discussion before implementation.

## Contribution Process

- Use GitHub issues for bugs, feature requests, and proposals.
- Use pull requests for changes; PRs should include tests and documentation updates as appropriate.
- See `CONTRIBUTING.md` for workflow details.

## Becoming a Maintainer

As the project grows, additional maintainers may be added.

Typical criteria:

- Sustained, high-quality contributions over time (code, reviews, docs, support)
- Demonstrated good judgment around safety, compatibility, and backwards-compatibility
- Willingness to help with triage and releases

The Maintainer will propose additions publicly (issue/discussion) and document maintainers in this file.

## Code of Conduct

All project participants must follow the CNCF Code of Conduct. See `CODE_OF_CONDUCT.md`.

