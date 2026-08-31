# Releasing

## Overview

Releases are fully automated via GitHub Actions. Pushing a tag matching `v*.*.*`
triggers [`.github/workflows/release.yml`](.github/workflows/release.yml), which:

1. Runs `govulncheck` to check for known vulnerabilities.
2. Builds and pushes the container image to [quay.io](https://quay.io).
3. Signs the image with [cosign](https://github.com/sigstore/cosign) using keyless Sigstore signing.
4. Attests [SLSA](https://slsa.dev) build provenance and an SBOM.
5. Creates a GitHub Release with the attestation artifacts.

## Required Tools

For release verification, you will need:

- **[`cosign`](https://docs.sigstore.dev/cosign/system_config/installation/)** — For verifying image signatures and
  attestations
- **[`gh`](https://cli.github.com/)** — GitHub CLI for verifying attestations and downloading release artifacts

## Prerequisites

- Push access to this repository.
- The Quay.io repository must exist and the `QUAY_USERNAME`/`QUAY_PASSWORD` secrets must be configured in the GitHub
  repository.

## Pre-Release Checklist

Before creating a release tag, ensure:

1. **Local build succeeds**:
   ```bash
   make docker-build
   ```

2. **Integration tests pass**:
    - Check the
      latest [CCM Integration Tests](https://github.com/cloudscale-ch/cloudscale-cloud-controller-manager/actions/workflows/ccm-integration-tests.yml)
      workflow run
    - All tests should be green on `main`
    - Optionally, trigger a manual run
      via ["Run workflow"](https://github.com/cloudscale-ch/cloudscale-cloud-controller-manager/actions/workflows/ccm-integration-tests.yml)
      on the Actions tab to test against the current HEAD

3. **`main` is in a releasable state**:
    - All intended PRs are merged
    - CI checks are passing

## Release Notes

GitHub Actions automatically generates release notes when the release workflow runs. After the workflow completes:

1. Navigate to the GitHub Release
2. Review the auto-generated notes
3. If necessary, amend with any special notes, migration guidance, or highlights that aren't captured automatically

## Regular release

1. **Create and push the tag** (use [SemVer](https://semver.org/)):

   ```bash
   git checkout main
   git pull
   git tag -a v1.2.3 -m 'Version v1.2.3'
   git push origin v1.2.3
   ```

2. **Wait for the workflow** to finish in the GitHub Actions tab.

3. **Verify the release**:

   ```bash
   export IMG=quay.io/cloudscalech/cloudscale-cloud-controller-manager:v1.2.3
   export ID_REGEXP='^https://github.com/cloudscale-ch/cloudscale-cloud-controller-manager/\.github/workflows/release\.yml@refs/tags/'
   export ISSUER=https://token.actions.githubusercontent.com
   ```

   ```bash
   # Verify the image signature
   cosign verify "$IMG" \
     --certificate-identity-regexp "$ID_REGEXP" \
     --certificate-oidc-issuer "$ISSUER"
   ```

   ```bash
   # Verify build provenance attestation (SLSA)
   gh attestation verify oci://"$IMG" --owner cloudscale-ch \
     --predicate-type https://slsa.dev/provenance/v1
   ```

   ```bash
   # Verify SBOM attestation
   gh attestation verify oci://"$IMG" --owner cloudscale-ch \
     --predicate-type https://spdx.dev/Document/v2.3
   ```

   ```bash
   # List all attestations attached to the image
   cosign tree "$IMG"
   ```

4. **Test installation on a test cluster**:

   ```bash
   # Create a test cluster with the released image
   export IMAGE=quay.io/cloudscalech/cloudscale-cloud-controller-manager:v1.2.3
   helpers/run-in-test-cluster

   # Verify CCM is running
   kubectl get daemonset cloudscale-cloud-controller-manager -n kube-system
   kubectl logs -l k8s-app=cloudscale-cloud-controller-manager -n kube-system

   # Cleanup
   helpers/cleanup
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

