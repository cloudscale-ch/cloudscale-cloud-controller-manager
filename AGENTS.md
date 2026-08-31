# AGENTS.md — CCM-specific guidance for AI agents

This file is a supplement for AI agents working on
`cloudscale-cloud-controller-manager` (CCM). For end-user documentation, read
[`README.md`](README.md) first.

## Related docs

- [`README.md`](README.md) — User-facing documentation
- [`docs/cluster-tagging.md`](docs/cluster-tagging.md) — Cluster tagging behavior
- [`docs/releasing.md`](docs/releasing.md) — Release process

## Do not edit (auto-generated)

None — CCM does not use code generation.

## What to run after a change

| You touched            | Run                          |
|------------------------|------------------------------|
| Any `*.go`             | `make lint-fix && make test` |
| `go.mod` / `go.sum`    | `make test`                  |
| `.github/workflows/`   | Verify with zizmor           |

## Where things live

| Component                       | Path                                      |
|---------------------------------|-------------------------------------------|
| CCM main entry point            | `cmd/cloudscale-cloud-controller-manager/`|
| CCM cloud provider logic        | `pkg/cloudscale_ccm/`                     |
| Internal utilities & helpers    | `pkg/internal/`                           |
| Integration tests (k8s + cloud) | `pkg/internal/integration/`               |
| k8test cluster setup (Ansible)  | `k8test/`                                 |
| Helper shell/Python scripts     | `helpers/`                                |
| Deployment manifests            | `deploy/`                                 |
| Example manifests               | `examples/`                               |

## Testing approach

- **Unit tests**: Next to code (`*_test.go`), run with `make test`.
- **Integration tests**: Require a real cloudscale.ch cluster; run with
  `make integration` (requires `CLOUDSCALE_API_TOKEN`).
- **Manual cluster testing**: `helpers/run-in-test-cluster` creates a test
  cluster and installs CCM.
- **Cleanup**: `helpers/cleanup` tears down any test cluster resources.

## Cloudscale SDK usage

- Import `github.com/cloudscale-ch/cloudscale-go-sdk/v10`.
- Pass `context.Context` with appropriate timeouts to all API calls.
- Handle errors idiomatically; do not panic on SDK errors.

## Logging style

- Use `k8s.io/klog/v2` for all logging.
- Follow Kubernetes message style guidelines:
  - Capitalized start, no trailing period.
  - Past tense (`"Created load balancer"`, not `"Creating load balancer"`).
  - Name the object type (`"Created FloatingIP"`, not `"Created"`).
  - Balanced key/value pairs.

## References

- [cloudscale-go-sdk](https://github.com/cloudscale-ch/cloudscale-go-sdk)
- [Kubernetes cloud-provider](https://github.com/kubernetes/cloud-provider)
- [Kubernetes controller patterns](https://github.com/kubernetes-sigs/controller-runtime)
