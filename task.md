# Align Go repository set up

Goal: Consistent Go Lint/CI/CD set up for all cloudscale Go repositories

## Tasks

- [ ] capcs-official [1] is used as the reference
- [ ] linting & fmt is done using `golangci-lint` with a custom golangci-lint format config
- [ ] tools are set up using make targets like it is done in capcs-official
- [ ] vet is added as a make target and called from appropriate make targets
- [ ] Makefile is set up similar to capcs-official with help docs and sections. But doesn't copy all verbatim, just what is actually used.
- [ ] github action workflows are split according to their tasks
- [ ] dependabot is configured like in capcs-official
- [ ] cloudscale-go-sdk v10 is used (yes, it is released)
- [ ] Go 1.27 is used (yes, it is released)
- [ ] release process is automated and most of the existing python code can be removed, if possible. make sure pre-release is still possible.
- [ ] releases are signed using cosign, and attestations are done (SBOM, build provenance, ..)
- [ ] an appropriate AGENTS.md is set up but not linked from README.md
- [ ] documentation is updated, also containing documentation for the release process

## TBD

golvulncheck wouldn't be too bad to be set up but I'm not 100% sure yet if it's a good idea to apply as a blocker to PRs.

## References

[1]: Cluster-API provider can be used as a reference: /Users/mweibel/code/cloudscale/capcs-official
