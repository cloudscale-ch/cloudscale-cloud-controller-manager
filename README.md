# Kubernetes Cloud Controller Manager for cloudscale.ch

> :warning: This is currently a work in-progress and not yet ready to be used.

Integrate your Kubernetes cluster with cloudscale.ch infrastructure, with our cloud controller manager (CCM).

Provides the following features:

- Automatically provisions load balancers for [`LoadBalancer` services](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer).
- Enriches `Node` metadata with information from our cloud.
- Updates `Node` state depending on the state of the underlying VM.

## Test Cluster

To test the CCM on a vanilla Kubernetes cluster, you can use `helpers/run-in-test-cluster`. This will create a small Kubernetes cluster at cloudscale.ch,
and install the current development version in it.

Note that you need a `CLOUDSCALE_API_TOKEN` for this to work, and this may incur costs on your side:

```bash
export CLOUDSCALE_API_TOKEN="..."
helpers/run-in-test-cluster
```

To clean the cluster up, run `helpers/cleanup`.

This is based on [k8test](https://github.com/cloudscale-ch/k8test), our in-house Kubernetes integration test utility.

## Kubernetes Support Policy

We aim to support the latest three minor Kubernetes releases. Older releases should work as well, but we do not test them automatically and we may decide not to fix bugs related to older releases.

For example, at the time of this writing 1.28.4 is the latest release, so we currently support 1.28.x, 1.27.x, and 1.26.x.

Tests are run reguarly against the latest patch of the previous three minor releases.
