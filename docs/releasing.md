# Releasing

## Overview

Releases are fully automated via GitHub Actions. Pushing a tag matching `v*.*.*`
triggers [`.github/workflows/release.yml`](.github/workflows/release.yml), which:

1. Runs `govulncheck` to gate the release on known vulnerabilities.
2. Builds and pushes the container image to [quay.io](https://quay.io).
3. Signs the image with [cosign](https://github.com/sigstore/cosign) using keyless Sigstore signing.
4. Attests [SLSA](https://slsa.dev) build provenance and an SBOM.
5. Creates a GitHub Release with the attestation artifacts.

## Prerequisites

- Push access to this repository.
- The Quay.io repository must exist and the `QUAY_USERNAME`/`QUAY_PASSWORD`
  secrets must be configured.

## Regular release

1. **Create and push the tag** (use [SemVer](https://semver.org/)):

   ```bash
   git checkout main
   git pull
   git tag v1.2.3
   git push origin v1.2.3
   ```

2. **Wait for the workflow** to finish in the GitHub Actions tab.

3. **Verify the release**:

   ```bash
   # Verify the image signature
   export IMG=quay.io/cloudscalech/cloudscale-cloud-controller-manager:v1.2.3
   cosign verify --certificate-identity-regexp \
     '^https://github.com/cloudscale-ch/cloudscale-cloud-controller-manager/\.github/workflows/release\.yml@refs/tags/' \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
     "$IMG"
   ```

   ```bash
   # Verify build provenance attestation
   gh attestation verify oci://"$IMG" --owner cloudscale-ch \
     --predicate-type https://slsa.dev/provenance/v1
   ```

   ```bash
   # Verify SBOM attestation
   gh attestation verify oci://"$IMG" --owner cloudscale-ch \
     --predicate-type https://spdx.dev/Document/v2.3
   ```

## Pre-release

For release candidates, alpha, or beta versions, append a hyphen and identifier
to the version tag:

```bash
git tag v1.2.3-alpha.1
git push origin v1.2.3-alpha.1
```

The workflow automatically detects the `-` in the tag name and marks the
GitHub Release as a **pre-release**. Any of the following suffixes are valid
per SemVer:

- `v1.2.3-alpha.1`
- `v1.2.3-beta.1`
- `v1.2.3-rc.1`

The container image is pushed with the exact same tag (e.g. `v1.2.3-alpha.1`),
and the same signing and attestation steps are applied.

## Troubleshooting

- **Workflow fails at `govulncheck`:** Address the reported vulnerabilities
  before retrying the release. Do not skip this step.
- **Image push fails:** Verify the Quay.io secrets are set correctly in the
  repository settings.
- **Signature verification fails:** Ensure you are using a recent version of
  `cosign` and that the workflow completed successfully.
