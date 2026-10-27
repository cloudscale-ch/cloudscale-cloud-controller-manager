# Releasing

## Overview

Releases are fully automated via GitHub Actions. Pushing a tag matching `v*.*.*`
triggers [`.github/workflows/release.yml`](../.github/workflows/release.yml), which:

1. Runs `govulncheck` to check for known vulnerabilities.
2. Builds and pushes the container image to [quay.io](https://quay.io).
3. Signs the image with [cosign](https://github.com/sigstore/cosign) using keyless Sigstore signing.
4. Attests [SLSA](https://slsa.dev) build provenance and an SBOM.
5. Renders the install manifest `config.yml` from [`deploy/latest.yml`](../deploy/latest.yml), pinned to the
   released image tag.
6. Creates a GitHub Release with the `kubectl apply` install command prepended to the generated notes, and
   `config.yml` and the SBOM attached.
7. Moves the `:latest` image tag to the new image, for full releases with the highest version only.

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

GitHub Actions automatically generates release notes when the release workflow runs. The workflow prepends a
"Configuration" section with the version-specific `kubectl apply` command to the generated notes. After the workflow
completes:

1. Navigate to the GitHub Release
2. Review the auto-generated notes
3. If necessary, amend with any special notes, migration guidance, or highlights that aren't captured automatically
4. Keep the "Configuration" section; the README points users to it

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

   ```bash
   # Verify the install manifest is attached and pinned to the release
   gh release download v1.2.3 --pattern config.yml --output - | grep "image:"

   # Verify :latest points to the released image (full releases only)
   docker buildx imagetools inspect "$IMG" --format '{{json .Manifest.Digest}}'
   docker buildx imagetools inspect quay.io/cloudscalech/cloudscale-cloud-controller-manager:latest \
     --format '{{json .Manifest.Digest}}'
   ```

4. **Test installation on a test cluster**:

   Requires `CLOUDSCALE_API_TOKEN`, SSH access to `cloudscale-ch/k8test`, python3, `docker` and `kubectl`.

   ```bash
   # Import the released image; the default (IMAGE_SOURCE=build) would build the local checkout instead
   export IMAGE=quay.io/cloudscalech/cloudscale-cloud-controller-manager:v1.2.3
   docker pull --platform linux/amd64 "$IMAGE"
   docker save "$IMAGE" -o image.tar
   IMAGE_SOURCE=import helpers/run-in-test-cluster

   # Verify CCM is running
   export KUBECONFIG=k8test/cluster/admin.conf
   kubectl get daemonset cloudscale-cloud-controller-manager -n kube-system
   kubectl logs -l k8s-app=cloudscale-cloud-controller-manager -n kube-system

   # Cleanup
   helpers/cleanup
   rm image.tar
   ```

## If the workflow fails

- **`govulncheck`**: nothing was published. Delete the tag (`git push --delete origin v1.2.3`, `git tag -d v1.2.3`),
  fix the findings and tag again.
- **Any later step**: delete the release if it exists (`gh release delete v1.2.3 --yes`, the tag is kept), then
  re-run the job.

## Pre-release

For release candidates, alpha, or beta versions, append a hyphen and identifier
to the version tag:

```bash
git tag -a v1.2.3-alpha.1 -m 'Version v1.2.3-alpha.1'
git push origin v1.2.3-alpha.1
```

The workflow automatically detects the `-` in the tag name and marks the
GitHub Release as a **pre-release**. Any of the following suffixes are valid
per SemVer:

- `v1.2.3-alpha.1`
- `v1.2.3-beta.1`
- `v1.2.3-rc.1`

The container image is pushed with the exact same tag (e.g. `v1.2.3-alpha.1`),
and the same signing and attestation steps are applied. Pre-releases also get
`config.yml` and the install command in the release notes, but the `:latest`
image tag is not moved.

## Backport release

To release a fix for an older version, tag the commit on its maintenance branch
instead of `main`:

```bash
git checkout release-1.1
git tag -a v1.1.4 -m 'Version v1.1.4'
git push origin v1.1.4
```

The same workflow runs. The `:latest` image tag is only moved when the tag is
the highest released version, so a backport leaves it on the newer release.

