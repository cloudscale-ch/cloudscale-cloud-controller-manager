package cloudscale_ccm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/kubeutil"
	"github.com/cloudscale-ch/cloudscale-go-sdk/v4"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
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

	// LoadBalancerVIPAddresses defines the virtual IP addresses through which
	// incoming traffic is received. this defaults to an automatically assigned
	// public IPv4 and IPv6 address.
	//
	// If you want to use a specific private subnet instead, to load balance
	// inside your cluster, you have to specify the subnet the loadbalancer
	// should bind to, and optionally what IP address it should use (if you
	// don't want an automatically assigned one).
	//
	// The value of this option is a list of JSON objects, as documented here:
	//
	// https://www.cloudscale.ch/en/api/v1#vip_addresses-attribute-specification
	//
	// By default, an empty list is set (to get a public address pair).
	//
	// This can currently not be changed and will cause an error if attempted,
	// as the loadbalancer would have to be recreated, causing potential
	// downtime, and a release of any address it held.
	//
	// To change the address it is recommended to create a new service
	// resources instead.
	LoadBalancerVIPAddresses = "k8s.cloudscale.ch/loadbalancer-vip-addresses"

	// LoadBalancerFloatingIPs assigns the given Floating IPs to the
	// load balancer. The expected value is a list of addresses of the
	// Floating IPs in CIDR notation. For example:
	//
	// ["5.102.150.123/32", "2a06:c01::123/128"]
	//
	// If any Floating IP address is assigned to multiple services via this
	// annotation, the CCM will refuse to update the associated services, as
	// this is considered a serious configuration issue that has to first be
	// resolved by the operator.
	//
	// While the service being handled needs to have a parseable Floating IP
	// config, the services it is compared to for conflict detection do not.
	//
	// Such services are skipped during conflict detection with the goal
	// of limiting the impact of config parse errors to the service being
	// processed.
	//
	// Floating IPs already assigned to the loadbalancer, but no longer
	// present in the annotations, stay on the loadbalancer until another
	// service requests them. This is due to the fact that it is not possible
	// to unassign Floating IPs to point to nowhere.
	//
	// The Floating IPs are only assigned to the LoadBalancer once it has
	// been fully created.
	LoadBalancerFloatingIPs = "k8s.cloudscale.ch/loadbalancer-floating-ips"

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

	// LoadBalancerForceHostname forces the CCM to report a specific hostname
	// to Kubernetes when returning the loadbalancer status, instead of
	// reporting the IP address(es).
	//
	// The hostname used should point to the same IP address that would
	// otherwise be reported. This is used as a workaround for clusters that
	// do not support status.loadBalancer.ingress.ipMode, and use `proxy` or
	// `proxyv2` protocol.
	//
	// For newer clusters, .status.loadBalancer.ingress.ipMode is automatically
	// set to "Proxy", unless LoadBalancerIPMode is set to "VIP"
	//
	// For more information about this workaround see
	// https://kubernetes.io/blog/2023/12/18/kubernetes-1-29-feature-loadbalancer-ip-mode-alpha/
	//
	// To illustrate, here's an example of a load balancer status shown on
	// a Kubernetes 1.29 service with default settings:
	//
	//    apiVersion: v1
	//    kind: Service
	//    ...
	//    status:
	//      loadBalancer:
	//        ingress:
	//          - ip: 45.81.71.1
	//          - ip: 2a06:c00::1
	//
	// Using the annotation causes the status to use the given value instead:
	//
	//    apiVersion: v1
	//    kind: Service
	//    metadata:
	//      annotations:
	//        k8s.cloudscale.ch/loadbalancer-force-hostname: example.org
	//    status:
	//      loadBalancer:
	//        ingress:
	//          - hostname: example.org
	//
	// If you are not using the `proxy` or `proxyv2` protocol, or if you are
	// on Kubernetes 1.30 or newer, you probly do not need this setting.
	//
	// See `LoadBalancerIPMode` below.
	LoadBalancerForceHostname = "k8s.cloudscale.ch/loadbalancer-force-hostname"

	// LoadBalancerIPMode defines the IP mode reported to Kubernetes for the
	// loadbalancers managed by this CCM. It defaults to "Proxy", where all
	// traffic destined to the load balancer is sent through the load balancer,
	// even if coming from inside the cluster.
	//
	// The older behavior, where traffic inside the cluster is directly
	// sent to the backend service, can be activated by using "VIP" instead.
	LoadBalancerIPMode = "k8s.cloudscale.ch/loadbalancer-ip-mode"

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

	// LoadBalancerSubnetLimit is a JSON list of subnet UUIDs that the
	// loadbalancer should use. By default, all subnets of a node are used:
	//
	// * `[]` means that anyone is allowed to connect (default).
	// * `["0769b7cf-199b-4d42-9fbd-9ab3d11d08da"]` only bind to this subnet.
	//
	// If set, the limit causes nodes that do not have a matching subnet
	// to be ignored. If no nodes with matching subnets are found, an
	// error is returned.
	//
	// This is an advanced feature, useful if you have nodes that are in
	// multiple private subnets.
	LoadBalancerListenerAllowedSubnets = "k8s.cloudscale.ch/loadbalancer-listener-allowed-subnets"
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

	result, err := l.loadBalancerStatus(serviceInfo, instance)
	if err != nil {
		return nil, true, fmt.Errorf(
			"unable to get load balancer state for %s: %w", service.Name, err)
	}

	return result, true, nil
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

	// Detect configuration issues and abort if they are found
	serviceInfo := newServiceInfo(service, clusterName)
	if err := l.ensureValidConfig(ctx, serviceInfo); err != nil {
		return nil, err
	}

	// Refuse to do anything if there are no nodes
	if len(nodes) == 0 {
		return nil, errors.New(
			"no valid nodes for service found, please verify there is " +
				"at least one that allows load balancers",
		)
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

	result, err := l.loadBalancerStatus(serviceInfo, actual.lb)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get load balancer state for %s: %w", service.Name, err)
	}

	return result, nil
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

	// Detect configuration issues and abort if they are found
	serviceInfo := newServiceInfo(service, clusterName)
	if err := l.ensureValidConfig(ctx, serviceInfo); err != nil {
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

	// Detect configuration issues and abort if they are found
	serviceInfo := newServiceInfo(service, clusterName)
	if err := l.ensureValidConfig(ctx, serviceInfo); err != nil {
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
func (l *loadbalancer) loadBalancerStatus(
	serviceInfo *serviceInfo,
	lb *cloudscale.LoadBalancer,
) (*v1.LoadBalancerStatus, error) {

	status := v1.LoadBalancerStatus{}

	// When forcing the use of a hostname, there's exactly one ingress item
	hostname := serviceInfo.annotation(LoadBalancerForceHostname)
	if len(hostname) > 0 {
		status.Ingress = []v1.LoadBalancerIngress{{Hostname: hostname}}

		return &status, nil
	}

	// Otherwise there as many items as there are addresses
	status.Ingress = make([]v1.LoadBalancerIngress, len(lb.VIPAddresses))

	var ipMode *v1.LoadBalancerIPMode
	ipModeAnnotation := serviceInfo.annotation(LoadBalancerIPMode)
	switch ipModeAnnotation {
	case "Proxy":
		ipMode = ptr.To(v1.LoadBalancerIPModeProxy)
	case "VIP":
		ipMode = ptr.To(v1.LoadBalancerIPModeVIP)
	default:
		return nil, fmt.Errorf(
			"unsupported IP mode: '%s', must be 'Proxy' or 'VIP'",
			ipModeAnnotation)
	}

	// On newer releases, we explicitly configure the IP mode
	supportsIPMode, err := kubeutil.IsKubernetesReleaseOrNewer(l.k8s, 1, 30)
	if err != nil {
		return nil, fmt.Errorf("failed to get load balancer status: %w", err)
	}

	for i, address := range lb.VIPAddresses {
		status.Ingress[i].IP = address.Address

		if supportsIPMode {
			status.Ingress[i].IPMode = ipMode
		}
	}

	return &status, nil
}

// ensureValidConfig ensures that the configuration can be applied at all,
// acting as a gate that ensures certain invariants before any changes are
// made.
//
// The general idea is that it's better to not make any chanages if the config
// is bad, rather than throwing errors later when some changes have already
// been made.
func (l *loadbalancer) ensureValidConfig(
	ctx context.Context, serviceInfo *serviceInfo) error {

	// Skip if the service is not supported by this CCM
	if supported, err := serviceInfo.isSupported(); !supported {
		return err
	}

	// If Floating IPs are used, make sure there are no conflicting
	// assignment across services.
	ips, err := l.findIPsAssignedElsewhere(ctx, serviceInfo)
	if err != nil {
		return fmt.Errorf("could not parse %s", LoadBalancerFloatingIPs)
	}

	if len(ips) > 0 {

		info := make([]string, 0, len(ips))
		for ip, service := range ips {
			info = append(info, fmt.Sprintf("%s->%s", ip, service))
		}

		return fmt.Errorf(
			"at least one Floating IP assigned to service %s is also "+
				"assigned to another service. Refusing to continue to avoid "+
				"flapping: %s",
			serviceInfo.Service.Name,
			strings.Join(info, ", "),
		)
	}

	return nil
}

// findIPsAssignedElsewhere lists other services and compares their Floating
// IPs with the ones found on the given service. If an IP is found to be
// assigned to two services, the IP and the name of the service are returned.
func (l *loadbalancer) findIPsAssignedElsewhere(
	ctx context.Context, serviceInfo *serviceInfo) (map[string]string, error) {

	ips, err := serviceInfo.annotationList(LoadBalancerFloatingIPs)
	if err != nil {
		return nil, err
	}

	if len(ips) == 0 {
		return nil, nil
	}

	conflicts := make(map[string]string, 0)

	// Unfortunately, there's no way to filter for the services that matter
	// here. The only available field selectors for services are
	// `metadata.name` and `metadata.namespace`.
	//
	// To support larger clusters, ensure to not load all services in a
	// single call.
	opts := metav1.ListOptions{
		Continue: "",
		Limit:    250,
	}

	svcs := l.k8s.CoreV1().Services("")
	for {
		services, err := svcs.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve services: %w", err)
		}

		for _, service := range services.Items {
			if service.Spec.Type != "LoadBalancer" {
				continue
			}
			if service.UID == serviceInfo.Service.UID {
				continue
			}

			otherInfo := newServiceInfo(&service, serviceInfo.clusterName)
			other, err := otherInfo.annotationList(LoadBalancerFloatingIPs)

			// Ignore errors loading the IPs of other services, as they would
			// not be configured either, if the current service is otherwise
			// okay, it should be able to continue.
			//
			// If this is not done, a single configuration error on a service
			// causes this function to err on all other services.
			if err != nil {
				continue
			}

			for _, ip := range other {
				if slices.Contains(ips, ip) {
					conflicts[ip] = service.Name
				}
			}
		}

		if services.Continue == "" {
			break
		}

		opts.Continue = services.Continue
	}

	return conflicts, nil
}
