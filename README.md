# Kubernetes Cloud Controller Manager for cloudscale.ch

> :warning: This is currently a work in-progress and not yet ready to be used.

Integrate your Kubernetes cluster with cloudscale.ch infrastructure, with our cloud controller manager (CCM).

- Automatically provisions load balancers for [`LoadBalancer`](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) services.
- Enriches `Node` metadata with information from our cloud.
- Updates `Node` state depending on the state of the underlying VM.

- Automatically provisions load balancers for [`LoadBalancer` services](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer).
- Enriches `Node` metadata with information from our cloud.
- Updates `Node` state depending on the state of the underlying VM.

## Kubernetes Support Policy

We support the three latest minor Kubernetes releases.

For example, if the current release is `1.29.0`, we support the following:

- `1.29.x`
- `1.28.x`
- `1.27.x`

Older releases should work as well, but we do not test them automatically and we may decide not to fix bugs related to older releases.

## Try It Out

To test the CCM on a vanilla Kubernetes cluster, you can use `helpers/run-in-test-cluster`. This will create a small Kubernetes cluster at cloudscale.ch,
and install the current development version in it.

```bash
export CLOUDSCALE_API_TOKEN="..."
helpers/run-in-test-cluster
```

You can access the created cluster as follows:

```bash
# Via kubectl
export KUBECONFIG = k8test/cluster/admin.conf
kubectl get nodes

# Via ssh
ssh ubuntu@<ip> -i k8test/cluster/ssh
```

To cleanup:

```bash
helpers/cleanup
```

> :warning: This may incur costs on your side. Clusters that are removed may also leave behind loadbalancers, if associated services are not removed first. Please look at https://control.cloudscale.ch after cleanup to ensure that everything was removed.

# Operator Manual

## Installation

### Configuring the Cluster

To install the CCM on a new cluster, you need to configure your `kubelet` to always use the following argument:

```bash
kubelet --cloud-provider=external
```

This should be persisted indefinitely, depending on your distribution. Feel free to open an issue if you have trouble locating the right place to do this in your setup.

A cluster careted this way **will start all nodes tainted** as follows:

```yaml
node.cloudprovider.kubernetes.io/uninitialized: true
```

This taint will be removed, once the CCM has initalized the nodes.

### Node IPs

With Kubernetes 1.29 and above, the nodes do not gain a node IP in Kubernetes, until the CCM has run.

This can be problematic for certain network plugins like Cilium, which expect this to exist. You may have to install such plugins after the CCM, or wait for them to heal after the CCM has been installed.

Alternatively you can configure `--node-ips` with `kubectl`, to explicitly set the IPs, but this may cause problems if the IPs set via `kubectl` differ from the IPs determined by the CCM.

See https://github.com/kubernetes/kubernetes/pull/121028

> :bulb: We recommend installing the CCM before installing the network plugin.

### Storing the API Token

To configure the CCM, the following secret needs to be configured:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cloudscale
  namespace: kube-system
stringData:
  access-token: "..."
```

Create a file like this named `cloudscale-api-token.yml`, with your token filled in, and run the following:

```bash
kubectl apply -f cloudscale-api-token.yml
```

You can get a token on https://control.cloudscale.ch. Be aware that you need a read/write token. The token should not be deleted while it is in use, so we recommend naming the token accordingly.

### Installing the CCM

To install the CCM, run the following command. This can be done as soon as the control-plane is reachable and the secret has been configured. The CCM will be installed on all control nodes, even if they are uninitialized:

To install the latest version:

```
kubectl apply -f https://github.com/cloudscale-ch/cloudscale-cloud-controller-manager/releases/latest/download/config.yml
```

To install a specific version, or to upgrade to a new version, have a look at the [list of releases](https://github.com/cloudscale-ch/cloudscale-cloud-controller-manager/releases].

Each release has a version-specific `kubectl apply` command in its release description.

### Existing Clusters

For existing clusters we recommend the following installation order:

1. [Storing the API Token](#storing-the-api-token)
2. [Installing the CCM](#installing-the-ccm)
3. [Configuring the Cluster](#configuring-the-cluster)

For step three you need to restart the kubelet once on each node (serially).

You can verify that the CCM is running, by having a look at the status of the `cloudscale-cloud-controller-manager` daemonset and its log.

At this point, `LoadBalancer` service resources can already be used, but the Node metadata will only be updated on the nodes once they have been tainted briefly as follows:

```bash
kubectl taint node <node> node.cloudprovider.kubernetes.io/uninitialized=true:NoSchedule
```

This taint should be immediately removed by the CCM and the metadata provided by the CCM should be added to the labels and addresses of the node.

You should also find a `ProviderID` spec on each node.

> :warning: These instructions may not be right for your cluster, so be sure to test this in a staging environment.

# Developer Manual

## Releases

Relases are not integrated into the CI process. This remains a manual step, as releases via tagging via GitHub tend to be finicky and hard to control precisely.
Instead there is a release CLI, which ensures that a release is tested, before uploading the tested container image to Quay.io and publishing a new release.

There are two ways to create a release:

1. From a separate branch (must be a pre-release).
2. From the main branch (for the real release).

To create releases, you need to install some Python dependencies (using Python 3.11+):

```bash
python3 -m venv venv
source venv/bin/activate

pip install poetry
poetry install
```

You will also need to set the following environment variables:

**`GITHUB_TOKEN`**

A fine-grained GitHub access token with the following properties:

- Limited to this repository.
- Actions: Read-Only.
- Commit statuses: Read-Only.
- Contents: Read and Write.

**`QUAY_BOT_USER` / `QUAY_BOT_PASS`**

Quay bot user with permission to write to the `cloudscalech/cloudscale-cloud-controller-manager` repository on quay.io.

You can then use `helpers/release create` to create a new release:

```bash
export GITHUB_TOKEN="github_pat_..."
export QUAY_BOT_USER="..."
export QUAY_BOT_PASS="..."

# Create a new minor release of the main branch
helpers/release create minor

# Create a new minor pre-release for a test branch
helpers/relese create minor --pre --ref test/branch
```
