package cloudscale_ccm

import (
	"context"
	"fmt"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/kubeutil"
	"github.com/cloudscale-ch/cloudscale-go-sdk/v4"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// Annotations used by the loadbalancer integration of cloudscale_ccm. Those
// are pretty much set in stone, once they are in a release, so do not change
// them, unless you know what you are doing.
const (
	// LoadBalancerUUID uniquely identifes the loadbalancer. This annotation
	// should not be provided by the customer, unless the adoption of an
	// existing load balancer is desired.
	//
	// In all other cases, this value is set by the CCM after creating the
	// load balancer, to ensure that we track it with a proper ID and not
	// a name that might change without our knowledge.
	LoadBalancerUUID = "k8s.cloudscale.ch/loadbalancer-uuid"

	// LoadBalancerConfigVersion is set by the CCM when it first handles a
	// service. It exists to allow future CCM changes and should not be
	// tampered with. Once set, it is not changed, unless there is an upgrade
	// path applied by the CCM.
	LoadBalancerConfigVersion = "k8s.cloudscale.ch/loadbalancer-config-version"

	// LoadBalancerName names the loadbalancer on creation, and renames it
	// later. Note that if the LoadBalancerUUID annotation exists, it takes
	// precedence over the name to match the load balancer.
	//
	// This annotation can be changed without downtime on an esablished
	// service, but it is not recommended.
	LoadBalancerName = "k8s.cloudscale.ch/loadbalancer-name"

	// LoadBalancerFlavor denotes the flavor used by the balancer. There is
	// currently only one flavor, lb-standard.
	//
	// This can currently not be changed and will cause an error if attempted.
	LoadBalancerFlavor = "k8s.cloudscale.ch/loadbalancer-flavor"

	// LoadBalancerZone defines the zone in which the load balancer is running.
	// This defaults to the zone of the Nodes (if there is only one).
	//
	// This can not be changed once the service is created.
	LoadBalancerZone = "k8s.cloudscale.ch/loadbalancer-zone"

	// LoadBalancerPoolAlgorithm defines the load balancing algorithm used
	// by the loadbalancer. See the API documentation for more information:
	//
	// https://www.cloudscale.ch/en/api/v1#pool-algorithms
	//
	// Defaults to `round_robin`.
	//
	// Changing this algorithm will on an established service causes downtime,
	// as all pools have to be recreated.
	LoadBalancerPoolAlgorithm = "k8s.cloudscale.ch/loadbalancer-pool-algorithm"

	// LoadBalancerPoolProtocol defines the protocol for all the pools of the
	// service. We are technically able to have different protocols for
	// different ports in a service, but as our options apart from `tcp` are
	// currently `proxy` and `proxyv2`, we go with Kubernetes's recommendation
	// to apply these protocols to all incoming connections the same way:
	//
	// https://kubernetes.io/docs/reference/networking/service-protocols/#protocol-proxy-special
	//
	// Supported protocols:
	//
	// https://www.cloudscale.ch/en/api/v1#pool-protocols
	//
	// An alternative approach might be to use the spec.ports.appService on the
	// service, with custom strings, should anyone require such a feature.
	//
	// Changing the pool protocol on an established service causes downtime,
	// as all pools have to be recreated.
	LoadBalancerPoolProtocol = "k8s.cloudscale.ch/loadbalancer-pool-protocol"

	// LoadBalancerHealthMonitorDelayS is the delay between two successive
	// checks, in seconds. Defaults to 2.
	//
	// Changing this annotation on an active service may lead to new
	// connections timing out while the monitor is updated.
	LoadBalancerHealthMonitorDelayS = "k8s.cloudscale.ch/loadbalancer-health-monitor-delay-s"

	// LoadBalancerHealthMonitorTimeoutS is the maximum time allowed for an
	// individual check, in seconds. Defaults to 1.
	//
	// Changing this annotation on an active service may lead to new
	// connections timing out while the monitor is updated.
	LoadBalancerHealthMonitorTimeoutS = "k8s.cloudscale.ch/loadbalancer-health-monitor-timeout-s"

	// LoadBalancerHealthMonitorDownThreshold is the number of the checks that
	// need to succeed before a pool member is considered up. Defaults to 2.
	LoadBalancerHealthMonitorUpThreshold = "k8s.cloudscale.ch/loadbalancer-health-monitor-up-threshold"

	// LoadBalancerHealthMonitorDownThreshold is the number of the checks that
	// need to fail before a pool member is considered down. Defaults to 3.
	//
	// Changing this annotation on an active service may lead to new
	// connections timing out while the monitor is updated.
	LoadBalancerHealthMonitorDownThreshold = "k8s.cloudscale.ch/loadbalancer-health-monitor-down-threshold"

	// LoadBalancerHealthMonitorType defines the approach the monitor takes.
	// (ping, tcp, http, https, tls-hello).
	//
	// See https://www.cloudscale.ch/en/api/v1#health-monitor-types
	//
	// Changing this annotation on an active service may lead to new
	// connections timing out while the monitor is recreated.
	LoadBalancerHealthMonitorType = "k8s.cloudscale.ch/loadbalancer-health-monitor-type"

	// LoadBalancerHealthMonitorHTTP configures details about the HTTP check.
	//
	// See https://www.cloudscale.ch/en/api/v1#http-attribute-specification
	//
	// Changing this annotation on an active service may lead to new
	// connections timing out while the monitor is updated.
	LoadBalancerHealthMonitorHTTP = "k8s.cloudscale.ch/loadbalancer-health-monitor-http"

	// LoadBalancerListenerProtocol defines the protocol used by the listening
	// port on the loadbalancer. Currently, only tcp is supported.
	//
	// See https://www.cloudscale.ch/en/api/v1#listener-protocols
	//
	// Changing this annotation on an established service may cause downtime
	// as the listeners are recreated.
	LoadBalancerListenerProtocol = "k8s.cloudscale.ch/loadbalancer-listener-protocol"

	// LoadBalancerListenerAllowedCIDRs is a JSON list of IP addresses that
	// should be allowed to access the load balancer. For example:
	//
	// * `[]` means that anyone is allowed to connect (default).
	// * `["1.1.1.1", "8.8.8.8"]` only the given addresses are allowed.
	//
	// Changing this annotation on an established service is considered safe.
	LoadBalancerListenerAllowedCIDRs = "k8s.cloudscale.ch/loadbalancer-listener-allowed-cidrs"

	// LoadBalancerListenerTimeoutClientDataMS denotes the milliseconds until
	// inactive client connections are dropped.
	//
	// Changing this annotation on an established service is considered safe.
	LoadBalancerListenerTimeoutClientDataMS = "k8s.cloudscale.ch/loadbalancer-timeout-client-data-ms"

	// LoadBalancerListenerTimeoutMemberConnectMS denotes the milliseconds
	// it should maximally take to connect to a pool member, before the
	// attempt is aborted.
	//
	// Changing this annotation on an established service is considered safe.
	LoadBalancerListenerTimeoutMemberConnectMS = "k8s.cloudscale.ch/loadbalancer-timeout-member-connect-ms"

	// LoadBalancerListenerTimeoutMemberDataMS denotes the milliseconds until
	// an inactive connection to a pool member is dropped.
	//
	// Changing this annotation on an established service is considered safe.
	LoadBalancerListenerTimeoutMemberDataMS = "k8s.cloudscale.ch/loadbalancer-timeout-member-data-ms"
)

type loadbalancer struct {
	lbs lbMapper
	srv serverMapper
	k8s kubernetes.Interface
}

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
//
// Implementations must treat the *v1.Service parameter as read-only and not
// modify it.
//
// Parameter 'clusterName' is the name of the cluster as presented to
// kube-controller-manager.
func (l *loadbalancer) GetLoadBalancer(
	ctx context.Context,
	clusterName string,
	service *v1.Service,
) (status *v1.LoadBalancerStatus, exists bool, err error) {

	serviceInfo := newServiceInfo(service, clusterName)
	if supported, _ := serviceInfo.isSupported(); !supported {
		return nil, false, nil
	}

	instance, err := l.lbs.findByServiceInfo(ctx, serviceInfo).AtMostOne()

	if err != nil {
		return nil, false, fmt.Errorf(
			"unable to get load balancer for %s: %w", service.Name, err)
	}

	if instance == nil {
		klog.InfoS(
			"loadbalancer does not exist",
			"Name", serviceInfo.annotation(LoadBalancerName),
			"Service", service.Name,
		)

		return nil, false, nil
	}

	return loadBalancerStatus(instance), true, nil
}

// GetLoadBalancerName returns the name of the load balancer. Implementations
// must treat the *v1.Service parameter as read-only and not modify it.
func (lb *loadbalancer) GetLoadBalancerName(
	ctx context.Context,
	clusterName string,
	service *v1.Service,
) string {
	name := newServiceInfo(service, clusterName).annotation(LoadBalancerName)

	klog.InfoS(
		"loaded loadbalancer name for service",
		"Name", name,
		"Service", service.Name,
	)

	return name
}

// EnsureLoadBalancer creates a new load balancer 'name', or updates the
// existing one. Returns the status of the balancer. Implementations must treat
// the *v1.Service and *v1.Node parameters as read-only and not modify them.
//
// Parameter 'clusterName' is the name of the cluster as presented to
// kube-controller-manager.
//
// Implementations may return a (possibly wrapped) api.RetryError to enforce
// backing off at a fixed duration. This can be used for cases like when the
// load balancer is not ready yet (e.g., it is still being provisioned) and
// polling at a fixed rate is preferred over backing off exponentially in
// order to minimize latency.
func (l *loadbalancer) EnsureLoadBalancer(
	ctx context.Context,
	clusterName string,
	service *v1.Service,
	nodes []*v1.Node,
) (*v1.LoadBalancerStatus, error) {

	// Skip if the service is not supported by this CCM
	serviceInfo := newServiceInfo(service, clusterName)
	if supported, err := serviceInfo.isSupported(); !supported {
		return nil, err
	}

	// Reconcile
	err := reconcileLbState(ctx, l.lbs.client, func() (*lbState, error) {
		// Get the desired state from Kubernetes
		servers, err := l.srv.mapNodes(ctx, nodes).All()
		if err != nil {
			return nil, fmt.Errorf(
				"unable to get load balancer for %s: %w", service.Name, err)
		}

		return desiredLbState(serviceInfo, nodes, servers)
	}, func() (*lbState, error) {
		// Get the current state from cloudscale.ch
		return actualLbState(ctx, &l.lbs, serviceInfo)
	})

	if err != nil {
		return nil, err
	}

	// Get the final state to show the status
	actual, err := actualLbState(ctx, &l.lbs, serviceInfo)
	if err != nil {
		return nil, err
	}

	// At creation annotate the service with necessary data
	version := serviceInfo.annotation(LoadBalancerConfigVersion)

	err = kubeutil.AnnotateService(ctx, l.k8s, serviceInfo.Service,
		LoadBalancerUUID, actual.lb.UUID,
		LoadBalancerConfigVersion, version,
		LoadBalancerZone, actual.lb.Zone.Slug,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to annotate service %s: %w", service.Name, err)
	}

	return loadBalancerStatus(actual.lb), nil
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
//
// Parameter 'clusterName' is the name of the cluster as presented to
// kube-controller-manager.
func (l *loadbalancer) UpdateLoadBalancer(
	ctx context.Context,
	clusterName string,
	service *v1.Service,
	nodes []*v1.Node,
) error {

	// Skip if the service is not supported by this CCM
	serviceInfo := newServiceInfo(service, clusterName)
	if supported, err := serviceInfo.isSupported(); !supported {
		return err
	}

	// Reconcile
	return reconcileLbState(ctx, l.lbs.client, func() (*lbState, error) {
		// Get the desired state from Kubernetes
		servers, err := l.srv.mapNodes(ctx, nodes).All()
		if err != nil {
			return nil, fmt.Errorf(
				"unable to get load balancer for %s: %w", service.Name, err)
		}

		return desiredLbState(serviceInfo, nodes, servers)
	}, func() (*lbState, error) {
		// Get the current state from cloudscale.ch
		return actualLbState(ctx, &l.lbs, serviceInfo)
	})
}

// EnsureLoadBalancerDeleted deletes the specified load balancer if it
// exists, returning nil if the load balancer specified either didn't exist or
// was successfully deleted.
//
// This construction is useful because many cloud providers' load balancers
// have multiple underlying components, meaning a Get could say that the lb
// doesn't exist even if some part of it is still laying around.
//
// Implementations must treat the *v1.Service parameter as read-only and not
// modify it.
//
// Parameter 'clusterName' is the name of the cluster as presented to
// kube-controller-manager.
func (l *loadbalancer) EnsureLoadBalancerDeleted(
	ctx context.Context,
	clusterName string,
	service *v1.Service,
) error {

	// Skip if the service is not supported by this CCM
	serviceInfo := newServiceInfo(service, clusterName)
	if supported, err := serviceInfo.isSupported(); !supported {
		return err
	}

	// Reconcile with a desired state of "nothing"
	return reconcileLbState(ctx, l.lbs.client, func() (*lbState, error) {
		return &lbState{}, nil
	}, func() (*lbState, error) {
		return actualLbState(ctx, &l.lbs, serviceInfo)
	})
}

// loadBalancerStatus generates the v1.LoadBalancerStatus for the given
// loadbalancer, as required by Kubernetes.
func loadBalancerStatus(lb *cloudscale.LoadBalancer) *v1.LoadBalancerStatus {

	status := v1.LoadBalancerStatus{}
	status.Ingress = make([]v1.LoadBalancerIngress, len(lb.VIPAddresses))

	for i, address := range lb.VIPAddresses {
		status.Ingress[i].IP = address.Address
	}

	return &status
}
