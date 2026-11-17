# OptiPod Console 0.3.1

This patch fixes recommendation persistence for long container names. Kubernetes limits the name segment of annotation keys to 63 bytes. OptiPod now preserves valid existing keys and uses a stable container-name digest only where a key would exceed that limit. The controller and console agent share the same mapping, including CPU and memory requests and limits. Recommendation mode continues to leave workload specifications unchanged.

Upgrade both the controller and the export agent to see all long-name recommendations. The console, agent and installer remain compatible with the 0.3 protocol and PostgreSQL schema 3; this release adds no database migration. Regenerate saved installation plans with the matching installer when upgrading.

The matched bundle contains console/installer chart and binaries `0.3.1`, controller chart `1.6.0-console.1`, console image `ghcr.io/sagart-cactus/optipod-console:0.3.1` and controller image `ghcr.io/sagart-cactus/optipod:console-0.3.1`.

## Verify the release

The release workflow signs both immutable image index digests with GitHub OIDC and Cosign, then verifies the exact workflow identity before publishing the release. The signed indexes include the platform images and BuildKit provenance/SBOM attestations. `IMAGES.json` records the source commit and image digests. The signed `SHA256SUMS` covers that manifest, all CLI archives and both charts.

Download `SHA256SUMS`, `SHA256SUMS.sigstore.json`, `IMAGES.json` and the required archives from this release. With Cosign installed, verify the signature before trusting the checksums:

```sh
identity='https://github.com/Sagart-cactus/optipod/.github/workflows/console-release.yml@refs/tags/console-v0.3.1'
issuer='https://token.actions.githubusercontent.com'
cosign verify-blob --certificate-identity "$identity" --certificate-oidc-issuer "$issuer" \
  --bundle SHA256SUMS.sigstore.json SHA256SUMS
# Download every listed archive before checking the complete manifest.
sha256sum -c SHA256SUMS
cosign verify --certificate-identity "$identity" --certificate-oidc-issuer "$issuer" \
  "$(jq -r .console IMAGES.json)"
cosign verify --certificate-identity "$identity" --certificate-oidc-issuer "$issuer" \
  "$(jq -r .controller IMAGES.json)"
```

Pin deployment images to the verified digests. Image-signature trust is specific to the issuing workflow; an organization's admission policy may require its own approved build identity in addition to upstream verification. See [Sigstore verification guidance](https://docs.sigstore.dev/cosign/verifying/verify/).

## Validation

Regression coverage validates every legal container-name length, distinct recommendations for multiple long-name containers, and unchanged workload specifications against a real Kubernetes API. The two-cluster Podman E2E exercises the recommendation processor, agent ingestion and PostgreSQL readback. Existing console E2E also covers installation, Prometheus and metrics-server, mTLS lifecycle, credential rotation, UI access and backup restoration. The PR records the completed test results.

For recommendation-only installations, retain Recommend policies, global dry-run and any existing RBAC/admission restrictions during the upgrade. The optional PostgreSQL subchart remains intended for evaluation; production requires operated backups, PITR and restore drills.
