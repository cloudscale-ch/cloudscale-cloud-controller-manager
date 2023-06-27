# Kubernetes Cloud Controller Manager for cloudscale.ch

Integrate your Kubernetes cluster with cloudscale.ch infrastructure, with our cloud controller manager (CCM).

Provides the following features:

* Automatically provisions load balancers for [`LoadBalancer` services](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer).
* Enriches `Node` metadata with information from our cloud.
* Updates `Node` state depending on the state of the underlying VM.
