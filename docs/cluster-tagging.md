# Tag CCM-managed load-balancer resources with a cluster identifier

## Context

The cloudscale CCM today creates and manages load-balancer resources (LoadBalancer,
LoadBalancerPool, LoadBalancerPoolMember, LoadBalancerListener,
LoadBalancerHealthMonitor) inside a cloudscale project, but stamps no identifying
metadata on them. Two consequences:

1. **No ownership trail.** When several Kubernetes clusters share one cloudscale
   project (a common pattern with Cluster API: management cluster + workload
   clusters in the same account), there is nothing on a cloudscale resource that
   says which cluster created it. Operator cleanup and audit are name-based and
   error-prone.
2. **Name-only lookup.** `lb_mapper.findByName` lists every LB in the project and
   matches by `Name`. A workload-cluster LB and a management-cluster LB that
   happen to share a name would collide. The CCM also discards `clusterName`
   beyond `serviceInfo` even though the upstream cloud-controller-manager
   framework already plumbs it via `--cluster-name`.

Goal: stamp every CCM-created load-balancer-side cloudscale resource with a
single tag `k8s-cluster-name=<resolved>` whenever a cluster name is resolvable.
Behavior is purely additive — no list operation is scoped by the tag in this
change, no existing untagged resource breaks, and clusters that cannot resolve a
name keep the present behavior.

Decisions already made:
- **Source order:** `--cluster-name` flag first (treat the framework default
  `kubernetes` as unset), fall back to a Node annotation, default
  `cluster.x-k8s.io/cluster-name`, env-overridable.
- **Lookup scope:** write only (no `WithTagFilter` on `List` calls).
- **Tag key:** `k8s-cluster-name`.
- **Tagged resources:** LoadBalancer, Pool, Listener, HealthMonitor, PoolMember.
  **Not** FloatingIP (user-owned).
- **Node disagreement:** skip tagging + emit `Warning` Service event.
- **Legacy resources:** backfill the tag on next reconcile, preserve other tags.

## Design

### Resolution helper (new)

New file `pkg/cloudscale_ccm/cluster_name.go`:

```go
const (
    // Tag key set on every CCM-managed LB-side resource when a cluster name
    // is resolvable. Cloudscale tag keys allow only [A-Za-z0-9_-].
    ClusterNameTagKey = "k8s-cluster-name"

    // Default Node annotation read when --cluster-name is unset. Matches the
    // Cluster API convention.
    DefaultClusterNameAnnotation = "cluster.x-k8s.io/cluster-name"

    // Env var to override the source annotation key (matches CLOUDSCALE_*
    // style used elsewhere in cloud.go).
    ClusterNameAnnotationEnv = "CLOUDSCALE_CLUSTER_NAME_ANNOTATION"
)

// resolveClusterName returns the cluster name to stamp into tags, or "" when
// no name can be resolved. Recorder is used to emit a Warning event on the
// service when nodes disagree.
func resolveClusterName(
    flagName string,
    nodes []*v1.Node,
    service *v1.Service,
    recorder record.EventRecorder,
) string
```

Resolution rules, in order:

1. If `flagName != ""` and `flagName != "kubernetes"` → return `flagName`.
   (`kubernetes` is the framework default and must not be tagged as such.)
2. Else read annotation key from env (`CLOUDSCALE_CLUSTER_NAME_ANNOTATION`,
   default `cluster.x-k8s.io/cluster-name`).
3. Collect distinct non-empty values from `node.Annotations[key]` across all
   `nodes`.
   - 0 values → return `""`.
   - 1 value → return it.
   - 2+ distinct values → `recorder.Eventf(service, "Warning",
     "ClusterNameTagAmbiguous", ...)` and return `""`.

Length sanitation: cloudscale allows up to 256 chars in a tag value; Kubernetes
DNS names cap at 253 — no truncation needed, but trim whitespace defensively.

### Plumbing into `serviceInfo`

`pkg/cloudscale_ccm/service_info.go` already carries `clusterName`. Add a
resolved cluster-tag value that the reconcile path consumes:

```go
type serviceInfo struct {
    Service          *v1.Service
    clusterName      string  // unchanged (raw flag value)
    resolvedCluster  string  // empty when not resolvable; only set by Ensure/Update paths
}

// clusterTags returns the cluster tag map to merge into resource requests, or
// nil when no cluster name was resolvable. Returning nil means "leave tags
// untouched" for legacy untagged resources.
func (s *serviceInfo) clusterTags() cloudscale.TagMap
```

`resolvedCluster` is populated in `EnsureLoadBalancer` and `UpdateLoadBalancer`
(both have `nodes`). For `GetLoadBalancer` and `EnsureLoadBalancerDeleted` it
stays empty — they're read/teardown paths and need no tag writes. Existing
tests that construct `serviceInfo` via `newServiceInfo(service, clusterName)`
keep working because the resolved field defaults to `""`.

### Reconcile diff: one-way tag merge

`pkg/cloudscale_ccm/reconcile.go` builds desired state and computes actions
against actual state. The diff for tags must be **subset-merge**, not equality:

- Desired = `{ClusterNameTagKey: resolvedCluster}` (or `nil` if unresolvable).
- Actual = whatever cloudscale returns on the resource.
- Drift rule: only the keys present in Desired matter. A diff exists iff a
  Desired key is missing from Actual or has a different value.
- When emitting a write, the payload `Tags` must be `Actual ∪ Desired` so user
  tags on legacy resources are preserved. This is what gives the backfill
  semantics for free.

This rule needs to be applied wherever the reconciler currently compares
resource attributes. Concretely: read `reconcile.go` (and the
per-resource compare helpers used by `desiredLbState` / `actualLbState`) and
add a `mergeClusterTag(desired, actual TagMap) TagMap` helper that the
diff/write code calls before constructing each `*Request`.

### Action plumbing

`pkg/internal/actions/actions.go` already wraps the SDK request types. Each
`Create*Action` builds a `cloudscale.<Resource>Request`. Today none sets
`Tags`. Two integration choices:

- **Option A (preferred):** make the actions accept a `Tags *cloudscale.TagMap`
  field, passed through from the reconciler. The reconciler computes the
  merged map per resource. Smaller blast radius — actions stay dumb.
- Option B: pass `serviceInfo` down to actions. Rejected: actions package
  doesn't depend on cloudscale_ccm and keeping that direction matters.

Actions to update (all in `pkg/internal/actions/actions.go`):

- `CreateLbAction` (line ~42-71): forward `Tags` into `LoadBalancerRequest`.
- `CreateLbPool` (~190): forward into `LoadBalancerPoolRequest`.
- `CreateLbPoolMember` (~221): forward into `LoadBalancerPoolMemberRequest`.
- `CreateLbListener` (~253): forward into `LoadBalancerListenerRequest`.
- `CreateLbHealthMonitor` (~371): forward into `LoadBalancerHealthMonitorRequest`.
- Add **Update** variants where backfill happens. Inspect existing rename/update
  actions; many likely exist already. Add `Tags` to each `*Request` they build
  when the field is set. **Do not touch** `FloatingIPs.Update` — FIPs stay
  untagged by this CCM.

All SDK request types here embed `TaggedResourceRequest{ Tags *TagMap }`
(see cloudscale-go-sdk/v6 `tags.go`).

### Where the resolved name gets stamped per resource type

In the reconciler:

- LoadBalancer: `Tags = merge(actual.lb.Tags, {k8s-cluster-name: <name>})`.
- Pool: same merge against `actual.pool.Tags`.
- Listener: same.
- HealthMonitor: same.
- PoolMember: same — note pool members are per-node, but the cluster tag is
  Service-resolved, so every member of a pool carries the same value.
- FloatingIP: untouched.

### What does **not** change

- `instances.go` (InstancesV2): doesn't create cloudscale resources. No change.
- `findByName` / `findByUUID` lookups in `lb_mapper.go` and `server_mapper.go`:
  no tag filter is added. List-then-match stays as is.
- `--allow-untagged-cloud` and `HasClusterID() bool`: irrelevant to this
  change. They gate the upstream startup check, not resource tags.
- `deploy/latest.yml`: optional follow-up — operators may add `--cluster-name`
  to the args list, but the deployment continues to work without it.

## Critical files

| File | Why |
|---|---|
| `pkg/cloudscale_ccm/cluster_name.go` *(new)* | Resolution helper + constants. |
| `pkg/cloudscale_ccm/cluster_name_test.go` *(new)* | Unit tests for the resolution rules. |
| `pkg/cloudscale_ccm/service_info.go` | Hold `resolvedCluster` and `clusterTags()`. |
| `pkg/cloudscale_ccm/loadbalancer.go` (lines 388-464, 494-539) | Call `resolveClusterName` at the top of `EnsureLoadBalancer` / `UpdateLoadBalancer`; stash on `serviceInfo`. |
| `pkg/cloudscale_ccm/reconcile.go` | Compute desired Tags per resource; merge into the write payload. Read carefully before editing — the exact integration point depends on how desired/actual are diffed. |
| `pkg/internal/actions/actions.go` (lines 42-71, 190-196, 221, 253, 371; plus any Update variants) | Accept and forward `Tags` on every LB-side create/update action. Skip the FloatingIPs update path. |
| `pkg/internal/actions/actions_test.go` | Assert `Tags` is sent on each create/update request type. |
| `pkg/cloudscale_ccm/reconcile_test.go` | Cover three cases: tag created on new LB, tag backfilled on existing untagged LB, user tags preserved across reconcile. |
| `pkg/cloudscale_ccm/loadbalancer_test.go` | Cover: flag set → tag written; flag "kubernetes" + CAPI annotation → tag written from annotation; conflicting node annotations → no tag + event. |

Existing utilities to reuse:
- `cloudscale.TagMap` and `TaggedResourceRequest` from cloudscale-go-sdk/v6 `tags.go`.
- `record.EventRecorder` already on `loadbalancer` struct
  (`loadbalancer.go:310`) — reuse for the disagreement event.
- `serviceInfo.annotation*` helpers stay untouched — Node annotations are
  read directly, not via this Service-scoped helper.

## Verification

1. **Unit tests** (`go test ./...`):
   - New `cluster_name_test.go` covers: empty flag → "", `kubernetes` flag → "",
     real flag → wins over annotation, missing flag + CAPI annotation on all
     nodes → annotation value, env-overridden annotation key, disagreement
     across nodes → "" + event recorded, env override empty/whitespace.
   - Reconcile tests cover create-with-tag, backfill, and user-tag preservation.
   - Action tests assert the request body includes `tags` JSON.

2. **Integration tests** (`pkg/internal/integration/`):
   - The suite already provisions real cloudscale LBs. Add one variant that
     sets `--cluster-name=ccm-int-test` and asserts the resulting LB has the
     `k8s-cluster-name=ccm-int-test` tag via the SDK `Get` call.
   - Add a second variant that simulates the CAPI case: pre-annotate the test
     Nodes with `cluster.x-k8s.io/cluster-name=ccm-int-test` and leave the
     flag unset; assert the same outcome.

3. **Manual smoke** on a real cluster:
   - Deploy with `--cluster-name=mycluster`. Create a `Service: LoadBalancer`.
     `cloudscale-cli load-balancer show <uuid>` should list
     `k8s-cluster-name: mycluster`. Same for the pool/listener/health-monitor.
   - Update the deployment to remove `--cluster-name`. Delete and re-create the
     Service. The LB should be created with no `k8s-cluster-name` tag
     (resolution returns "").
   - Backfill check: start a Service before deploying this change (untagged
     LB), then upgrade the CCM with `--cluster-name=mycluster` and trigger a
     reconcile (e.g. change `LoadBalancerPoolAlgorithm` annotation). The
     existing LB should gain `k8s-cluster-name=mycluster` without losing any
     prior tags.

4. **Lint / build:** `go vet ./...`, `golangci-lint run` (if present),
   `go build ./...`.

## Out of scope (deliberate)

- `WithTagFilter` on `List` calls (Phase 2; opens questions about untagged
  legacy fallback that we do not want to answer in this change).
- Tagging cloudscale Servers (the CCM does not create them — CAPI or the
  operator does).
- Tagging FloatingIPs.
- Adding a `--cluster-name` CLI flag wiring: it already exists upstream via
  `KubeCloudShared.ClusterName`; operators set it on the CCM args list.