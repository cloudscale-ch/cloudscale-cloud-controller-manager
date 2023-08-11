# Kubernetes Cloud Controller Manager for cloudscale.ch

Integrate your Kubernetes cluster with cloudscale.ch infrastructure, with our cloud controller manager (CCM).

Provides the following features:

* Automatically provisions load balancers for [`LoadBalancer` services](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer).
* Enriches `Node` metadata with information from our cloud.
* Updates `Node` state depending on the state of the underlying VM.

## Test Cluster

To test the code on a vanilla Kubernetes cluster, you can use `helpers/run-in-test-cluster`. Note that you need a `CLOUDSCALE_API_TOKEN` for this to work, and this may incur costs on your side.

To clean the cluster up, run `helpers/cleanup`.

This is based on [k8test](https://github.com/cloudscale-ch/k8test), our in-house Kubernetes integration test utility.
