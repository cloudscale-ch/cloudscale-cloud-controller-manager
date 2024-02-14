# Kubernetes Cloud Controller Manager for cloudscale.ch

> :warning: This is currently a preview and not yet ready for production.

Integrate your Kubernetes cluster with cloudscale.ch infrastructure, with our cloud controller manager (CCM).

- Automatically provisions load balancers for [`LoadBalancer` services](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer).
- Enriches `Node` metadata with information from our cloud.
- Updates `Node` state depending on the state of the underlying VM.

## Kubernetes Support Policy

We support the three latest minor Kubernetes releases.

For example, if the current release is `1.29.0`, we support the following:

- `1.29.x`
- `1.28.x`
- `1.27.x`

Older releases should work as well, but we do not test them automatically, and we may decide not to fix bugs related to older releases.

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
export KUBECONFIG=k8test/cluster/admin.conf
kubectl get nodes

# Via ssh
ssh ubuntu@<ip> -i k8test/cluster/ssh
```

To clean up:

```bash
helpers/cleanup
```

> :warning: This may incur costs on your side. Clusters that are removed may also leave behind load balancers, if associated services are not removed first. Please look at https://control.cloudscale.ch after cleanup to ensure that everything was removed.

### Node Metadata Example

Once installed, the CCM will enrich nodes with metadata like the following:

```yaml
metadata:
  labels:
    node.kubernetes.io/instance-type: plus-32-16
    topology.kubernetes.io/region: lpg
    topology.kubernetes.io/zone: lpg1
spec:
  providerID: cloudscale://<server-uuid>
status:
  addresses:
    - address: k8test-worker-1
      type: Hostname
    - address: 5.102.148.123
      type: ExternalIP
    - address: 2a06:c01:1000:1165::123
      type: ExternalIP
    - address: 10.1.1.123
      type: InternalIP
```

### LoadBalancer Example

To run a simple load balanced service, you can use the following example:

```bash
kubectl create deployment hello \
  --image=nginxdemos/hello:plain-text \
  --replicas=2
kubectl expose deployment hello \
  --name=hello \
  --type=LoadBalancer \
  --port=80 \
  --target-port=80 \
```

Afterward, wait for the external IP to become available:

```bash
kubectl get service hello --watch
```

Details and some progress messages are visible here:

```bash
kubectl describe service hello
```

To check the CCM log, run the following:

```bash
kubectl logs -l k8s-app=cloudscale-cloud-controller-manager -n kube-system
```

Once the external IP is available, you can use it to check the result:

```bash
$ curl 5.102.148.123
Server address: 5.102.148.123:80
Server name: hello-7766f96cd-m7pvk
Date: 05/Jan/2024:10:20:18 +0000
URI: /
Request ID: dbe6be294e3280b6ff3b919abf20e9f9
```

# Operator Manual

## Installation

### Configuring the Cluster

To install the CCM on a new cluster, you need to configure your `kubelet` to always use the following argument:

```bash
kubelet --cloud-provider=external
```

This should be persisted indefinitely, depending on your distribution. Feel free to open an issue if you have trouble locating the right place to do this in your setup.

A cluster created this way **will start with all nodes tainted** as follows:

```yaml
node.cloudprovider.kubernetes.io/uninitialized: true
```

This taint will be removed, once the CCM has initialized the nodes.

### Node IPs

With Kubernetes 1.29 and above, the nodes do not gain a node IP in Kubernetes, until the CCM has run.

This can be problematic for certain network plugins like Cilium, which expect this to exist. You may have to install such plugins after the CCM, or wait for them to heal after the CCM has been installed.

Alternatively, you can configure `--node-ips` with `kubectl`, to explicitly set the IPs, but this may cause problems if the IPs set via `kubectl` differ from the IPs determined by the CCM.

See https://github.com/kubernetes/kubernetes/pull/121028

> :bulb: We recommend installing the CCM before installing the network plugin.

### Storing the API Token

To configure the CCM, the following secret needs to be configured:

```bash
kubectl create secret generic cloudscale \
  --from-literal=access-token='...' \
  --namespace kube-system
```

You can get a token on https://control.cloudscale.ch. Be aware that you need a read/write token. The token should not be deleted while it is in use, so we recommend naming the token accordingly.

### Installing the CCM

To install the CCM, run the following command. This can be done as soon as the control-plane is reachable and the secret has been configured. The CCM will be installed on all control nodes, even if they are uninitialized:

To install the latest version:

```
kubectl apply -f https://github.com/cloudscale-ch/cloudscale-cloud-controller-manager/releases/latest/download/config.yml
```

To install a specific version, or to upgrade to a new version, take a look at the [list of releases](https://github.com/cloudscale-ch/cloudscale-cloud-controller-manager/releases).

Each release has a version-specific `kubectl apply` command in its release description.

### Existing Clusters

For existing clusters, we recommend the following installation order:

1. [Storing the API Token](#storing-the-api-token)
2. [Installing the CCM](#installing-the-ccm)
3. [Configuring the Cluster](#configuring-the-cluster)

For step three, you need to restart the kubelet once on each node (serially).

You can verify that the CCM is running, by taking a look at the status of the `cloudscale-cloud-controller-manager` daemon set and its log.

At this point, `LoadBalancer` service resources can already be used, but the Node metadata will only be updated on the nodes once they have been tainted briefly as follows:

```bash
kubectl taint node <node> node.cloudprovider.kubernetes.io/uninitialized=true:NoSchedule
```

This taint should be immediately removed by the CCM and the metadata provided by the CCM should be added to the labels and addresses of the node.

You should also find a `ProviderID` spec on each node.

> :warning: These instructions may not be right for your cluster, so be sure to test this in a staging environment.

### LoadBalancer Service Configuration

You can influence the way services of type `LoadBalancer` are created by the CCM. To do so, set annotations on the service resource:

```yaml
apiversion: v1
kind: Service
metadata:
  annotations:
    k8s.cloudscale.ch/loadbalancer-listener-allowed-cidrs: '["1.2.3.0/24"]'
    k8s.cloudscale.ch/loadbalancer-floating-ips: '["1.2.3.4/32"]'
```

The full set of configuration toggles can be found in the [`pkg/cloudscale_ccm/loadbalancer.go`](pkg/cloudscale/ccm/loadbalancer.go) file.

These annotations are all optional, as they come with reasonable defaults.

### External Traffic Policy: Local

By default, Kubernetes adds an extra hop between load balancer and the pod that handles a packet. The load balancer sends packets to all nodes and the nodes implement balancing using NAT, adding an additional hop.

In some cases, the extra hop is undesireable or unnecessary. In this case, the external traffic policy can be set to local:

```yaml
apiVersion: v1
kind: Service
spec:
  externalTrafficPolicy: Local
```

With this policy, the load balancer only sends traffic to nodes that have at least one of the necessary pods, and Kubernetes will only send traffic to the pods local to the node.

Note that the default value of `Cluster` is generally a good default and changing the external traffic policy to `Local` should only be made if there are clear benefits.

### Client Source IP

Because traffic setup via CCM goes through our load balancers, you do not see the client source IP. To get access to the client's IP, you can configure your service to use the `proxy` or `proxyv2` protocol, which is supported by web servers like [NGINX](https://docs.nginx.com/nginx/admin-guide/load-balancer/using-proxy-protocol/).

```yaml
apiversion: v1
kind: Service
metadata:
  annotations:
    k8s.cloudscale.ch/loadbalancer-pool-protocol: proxyv2
```

See https://kubernetes.io/docs/reference/networking/service-protocols/#protocol-proxy-special

### Impact of Service Changes

The CCM reacts to service changes by changing the load balancer configuration.

Depending on the change, this can have a bigger or a smaller impact. While we try to be as efficient and non-disruptive as possible, we often have to apply generic actions to safely get to the desired state.

What follows is a list of changes that you might want to apply to an existing service, with a description of the expected impact.

You can get detailed information about each annotation here in the [`pkg/cloudscale_ccm/loadbalancer.go`](pkg/cloudscale/ccm/loadbalancer.go) file.

> :warning: We recommend using testing environments and maintenance windows to avoid surprises when changing configuration.

#### No Impact

The following annotations can be changed safely at any time, and should not impact any active or new connections:

- `k8s.cloudscale.ch/loadbalancer-timeout-client-data-ms`
- `k8s.cloudscale.ch/loadbalancer-timeout-member-connect-ms`
- `k8s.cloudscale.ch/loadbalancer-timeout-member-data-ms`
- `k8s.cloudscale.ch/loadbalancer-name` (though we recommend to not change it).

#### Minimal Impact

Changes to the CIDRs is generally safe, but may impact new connections if they do not match the CIDR:

- `k8s.cloudscale.ch/loadbalancer-listener-allowed-cidrs`

Floating IP changes are also safe, but they should be applied with care:

- `k8s.cloudscale.ch/loadbalancer-floating-ips`

Changes to following annotations may lead to new connections timing out until the change is complete:

- `k8s.cloudscale.ch/loadbalancer-health-monitor-delay-s`
- `k8s.cloudscale.ch/loadbalancer-health-monitor-timeout-s`
- `k8s.cloudscale.ch/loadbalancer-health-monitor-up-threshold`
- `k8s.cloudscale.ch/loadbalancer-health-monitor-down-threshold`
- `k8s.cloudscale.ch/loadbalancer-health-monitor-type`
- `k8s.cloudscale.ch/loadbalancer-health-monitor-http`

##### Listener Port Changes

Changes to the outward bound service port have a downtime ranging from 15s to 120s, depending on the action. Since the name of the port is used to avoid expensive pool recreation, the impact is minimal if the port name does not change.

For example, the following port 80 to port 8080 change should cause downtime of no more than 15s, as the implicit name of "" is not changed:

<table>
<thead><tr><th>Before</th><th>After</th></tr></thead>
<tbody><tr><td>

```yaml
apiVersion: v1
kind: Service
spec:
  ports:
    - port: 80
      protocol: TCP
      targetPort: 80
```

</td>
<td>

```yaml
apiVersion: v1
kind: Service
spec:
  ports:
    - port: 8080
      protocol: TCP
      targetPort: 80
```

</td></tr></tbody></table>

If the name is made explicit, the same rule applies and we should not see downtime of more than 15s:

<table>
<thead><tr><th>Before</th><th>After</th></tr></thead>
<tbody><tr><td>

```yaml
apiVersion: v1
kind: Service
spec:
  ports:
    - port: 80
      protocol: TCP
      targetPort: 80
      name: http
```

</td>
<td>

```yaml
apiVersion: v1
kind: Service
spec:
  ports:
    - port: 8080
      protocol: TCP
      targetPort: 80
      name: http
```

</td></tr></tbody></table>

Adding and removing ports should also not impact any ports that are unaffected by the change.

However, the following change causes a pool to be recreated and therefore a downtime of 60s-120s is estimated:

<table>
<thead><tr><th>Before</th><th>After</th></tr></thead>
<tbody><tr><td>

```yaml
apiVersion: v1
kind: Service
spec:
  ports:
    - port: 80
      protocol: TCP
      targetPort: 80
      name: http
```

</td>
<td>

```yaml
apiVersion: v1
kind: Service
spec:
  ports:
    - port: 443
      protocol: TCP
      targetPort: 80
      name: https
```

</td></tr></tbody></table>

Smae goes for this change, where the default name of "" is changed. This is the most surprising example and underscores why it is generally a good idea to plan some maintenance, even if the expected impact is minor:

<table>
<thead><tr><th>Before</th><th>After</th></tr></thead>
<tbody><tr><td>

```yaml
apiVersion: v1
kind: Service
spec:
  ports:
    - port: 80
      protocol: TCP
      targetPort: 80
```

</td>
<td>

```yaml
apiVersion: v1
kind: Service
spec:
  ports:
    - port: 80
      protocol: TCP
      targetPort: 80
      name: http
```

</td></tr></tbody></table>

#### Considerable Impact

Changes to the following annotations causes pools to be recreated and cause an estimated downtime of 60s-120s.

- `k8s.cloudscale.ch/loadbalancer-pool-algorithm`
- `k8s.cloudscale.ch/loadbalancer-pool-protocol`
- `k8s.cloudscale.ch/loadbalancer-listener-allowed-subnets`

#### Major Impact

Changes to the following annotations are not allowed by the CCM and can only be implemented by deleting and re-creating the service. This is due to the fact that these changes would cause a load balancer to be re-created, causing major downtime and the loss of the currently associated IP address (with the exception of the Floating IP):

- `k8s.cloudscale.ch/loadbalancer-flavor` (may be supported in the future).
- `k8s.cloudscale.ch/loadbalancer-zone`
- `k8s.cloudscale.ch/loadbalancer-vip-addresses`

# Developer Manual

## Releases

Releases are not integrated into the CI process. This remains a manual step, as releases via tagging via GitHub tend to be finicky and hard to control precisely.
Instead, there is a release CLI, which ensures that a release is tested, before uploading the tested container image to Quay.io and publishing a new release.

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

**`QUAY_USER` / `QUAY_PASS`**

Quay user with permission to write to the `cloudscalech/cloudscale-cloud-controller-manager` repository on quay.io.

You can then use `helpers/release create` to create a new release:

```bash
export GITHUB_TOKEN="github_pat_..."
export QUAY_USER="..."
export QUAY_PASS="..."

# Create a new minor release of the main branch
helpers/release create minor

# Create a new minor pre-release for a test branch
helpers/release create minor --pre --ref test/branch
```
