# OptiPod Console 0.3.0

Install a shared console with PostgreSQL, then onboard clusters through the administrator wizard and resumable CLI. This release adds cluster-bound mTLS enrollment and renewal, metrics/storage/network preflight, Helm and GitOps installation, scoped Recommend policy generation, access administration, lifecycle controls, fleet templates, notifications and recovery diagnostics.

The release contains CLI binaries for Linux and macOS on amd64 and arm64, checksums, the console chart `0.3.0`, and a compatible optimizer chart `1.6.0-console.0`. The optimizer bundle is built from the same reviewed commit and uses image tag `console-0.3.0`. Existing optimizers are left under their existing management.

Verify `SHA256SUMS`, extract the archive for your platform, place `optipod-console` on your PATH and install Helm 3 or 4. Follow [the installation guide](https://github.com/Sagart-cactus/optipod/blob/main/docs/console-onboarding.yaml).

Images:

- `ghcr.io/sagart-cactus/optipod-console:0.3.0`
- `ghcr.io/sagart-cactus/optipod:console-0.3.0`

Charts:

- `oci://ghcr.io/sagart-cactus/charts/optipod-console:0.3.0`
- `oci://ghcr.io/sagart-cactus/charts/optipod:1.6.0-console.0`

The installer supports Kubernetes 1.33–1.35; automated real-cluster coverage uses 1.35. Workload onboarding stays in Recommend mode. The optional PostgreSQL subchart is for evaluation; production requires operated backups, PITR and restore drills. Schema version 3 requires a compatible console binary and a verified recovery point before upgrading.
