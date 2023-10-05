package cloudscale_ccm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"time"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/actions"
	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/compare"
	"github.com/cloudscale-ch/cloudscale-go-sdk/v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type lbState struct {
	lb *cloudscale.LoadBalancer

	// Pool pointers are used to refer to members by pool, therefore use a
	// pointer here as well, to not accidentally copy the struct.
	pools   []*cloudscale.LoadBalancerPool
	members map[*cloudscale.LoadBalancerPool][]cloudscale.
		LoadBalancerPoolMember

	// Though not currently used that way, monitors and listeners are not
	// necessarily bound to any given pool. Monitors and listeners not bound
	// to a pool will be added to the `monitors[nil]` / `listeners[nil]` list
	// in the future.
	monitors map[*cloudscale.LoadBalancerPool][]cloudscale.
			LoadBalancerHealthMonitor
	listeners map[*cloudscale.LoadBalancerPool][]cloudscale.
			LoadBalancerListener
}

func newLbState(lb *cloudscale.LoadBalancer) *lbState {
	return &lbState{
		lb:    lb,
		pools: make([]*cloudscale.LoadBalancerPool, 0),
		members: make(
			map[*cloudscale.LoadBalancerPool][]cloudscale.LoadBalancerPoolMember),
		monitors: make(
			map[*cloudscale.LoadBalancerPool][]cloudscale.LoadBalancerHealthMonitor),
		listeners: make(
			map[*cloudscale.LoadBalancerPool][]cloudscale.LoadBalancerListener),
	}
}

// desiredLbState computes the state we want to see with the given service
// and nodes. Note that nodes/servers should be a 1:1 mapping, so that
// the first node points to the first server, and so on.
func desiredLbState(
	serviceInfo *serviceInfo,
	nodes []*v1.Node,
	servers []cloudscale.Server,
) (*lbState, error) {

	// This would indicate a programming error somewhere
	if len(nodes) != len(servers) {
		return nil, fmt.Errorf("bad node to server mapping")
	}

	// Get the zone of the load balancer, either from annotation, or by
	// looking at the nodes.
	zone := serviceInfo.annotation(LoadBalancerZone)
	if zone == "" {
		for _, s := range servers {
			if zone != "" && zone != s.Zone.Slug {
				return nil, fmt.Errorf(
					"no loadbalancer zone set and nodes in multiple zones",
				)
			}
			zone = s.Zone.Slug
		}
	}

	s := newLbState(&cloudscale.LoadBalancer{
		Name: serviceInfo.annotation(LoadBalancerName),
		// TODO add support for specificying VIP addresses explicitly
		VIPAddresses: []cloudscale.VIPAddress{},
		Flavor: cloudscale.LoadBalancerFlavorStub{
			Slug: serviceInfo.annotation(LoadBalancerFlavor),
		},
		ZonalResource: cloudscale.ZonalResource{
			Zone: cloudscale.Zone{Slug: zone},
		},
	})

	// Each service port gets its own pool
	algorithm := serviceInfo.annotation(LoadBalancerPoolAlgorithm)
	protocol := serviceInfo.annotation(LoadBalancerPoolProtocol)

	for _, port := range serviceInfo.Service.Spec.Ports {

		if port.Protocol != "TCP" {
			return nil, fmt.Errorf(
				"service %s: cannot use %s for %d, only TCP is supported",
				serviceInfo.Service.Name,
				port.Protocol,
				port.Port)
		}

		nodePort := int(port.NodePort)
		if nodePort == 0 {
			return nil, fmt.Errorf(
				"service %s: unknown port: %d",
				serviceInfo.Service.Name,
				port.NodePort)
		}

		monitorPort := nodePort
		if serviceInfo.Service.Spec.ExternalTrafficPolicy == "Local" {
			if serviceInfo.Service.Spec.HealthCheckNodePort > 0 {
				monitorPort = int(serviceInfo.Service.Spec.HealthCheckNodePort)
			}
		}

		pool := cloudscale.LoadBalancerPool{
			Name:      poolName(port.Protocol, port.Port),
			Algorithm: algorithm,
			Protocol:  protocol,
		}
		s.pools = append(s.pools, &pool)

		// For each server and private address, we need to add a pool member
		//
		// TODO add support for limiting this to a specific subnet (per default
		// all private networks are added).
		// TODO add support for floating IPs.
		for _, server := range servers {
			for _, iface := range server.Interfaces {

				// There's currently no support to load balance "to public"
				if iface.Type == "public" {
					continue
				}

				// Create a pool member for each address
				for _, addr := range iface.Addresses {

					name := poolMemberName(addr.Address, nodePort)
					s.members[&pool] = append(s.members[&pool],
						cloudscale.LoadBalancerPoolMember{
							Name:         name,
							Enabled:      true,
							Address:      addr.Address,
							Subnet:       addr.Subnet,
							ProtocolPort: nodePort,
							MonitorPort:  monitorPort,
						},
					)
				}
			}
		}

		// If there are no pool members, return an error. It would be possible
		// to just put a load balancer up that has no function, but it seems
		// more useful to err instead, as there's likely something wrong.
		if len(s.members[&pool]) == 0 {
			return nil, fmt.Errorf(
				"service %s: no private address found on any node",
				serviceInfo.Service.Name)
		}

		// Add a health monitor for each pool
		monitor, err := healthMonitorForPort(serviceInfo)
		if err != nil {
			return nil, err
		}

		s.monitors[&pool] = append(s.monitors[&pool], *monitor)

		// Add a listener for each pool
		listener, err := listenerForPort(serviceInfo, int(port.Port))
		if err != nil {
			return nil, err
		}

		s.listeners[&pool] = append(s.listeners[&pool], *listener)
	}

	return s, nil
}

func actualLbState(
	ctx context.Context,
	l *lbMapper,
	serviceInfo *serviceInfo,
) (*lbState, error) {

	// Get the loadbalancer
	lb, err := l.findByServiceInfo(ctx, serviceInfo).AtMostOne()
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get load balancer for %s: %w",
			serviceInfo.Service.Name, err)
	}
	if lb == nil {
		return &lbState{}, nil
	}

	s := newLbState(lb)

	// Keep track of pool UUIDs (this can be removed once the load balancer
	// info is included in listener/monitor list calls).
	poolUUIDs := make(map[string]bool)

	// Load all monitors/listeners first (may be from other load balancers)
	monitors, err := l.client.LoadBalancerHealthMonitors.List(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"lb state: failed to load monitors: %w", err)
	}

	listeners, err := l.client.LoadBalancerListeners.List(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"lb state: failed to load listeners: %w", err)
	}

	// Gather pools and members
	pools, err := l.client.LoadBalancerPools.List(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"lb state: failed to load pools: %w", err)
	}

	for _, pool := range pools {
		p := pool

		if p.LoadBalancer.UUID != lb.UUID {
			continue
		}

		s.pools = append(s.pools, &p)
		poolUUIDs[p.UUID] = true

		s.members[&p], err = l.client.LoadBalancerPoolMembers.List(ctx, p.UUID)
		if err != nil {
			return nil, fmt.Errorf(
				"lbstate: failed to load members for %s: %w", p.UUID, err)
		}

		for _, m := range monitors {
			if m.Pool.UUID != p.UUID {
				continue
			}

			s.monitors[&p] = append(s.monitors[&p], m)
		}

		for _, l := range listeners {
			if l.Pool.UUID != p.UUID {
				continue
			}

			s.listeners[&p] = append(s.listeners[&p], l)
		}
	}

	return s, nil
}

// nextLbActions returns a list of actions to take to ensure a desired
// loadbalancer state is reached.
func nextLbActions(
	desired *lbState, actual *lbState) ([]actions.Action, error) {

	next := make([]actions.Action, 0)

	// Some state has to be given, even if empty
	if desired == nil {
		return next, errors.New("no desired state given")
	}

	if actual == nil {
		return next, errors.New("no desired state given")
	}

	delete := func(url string) {
		next = append(next,
			actions.DeleteResource(url),
			actions.Sleep(500*time.Millisecond))
	}

	// Keys define the values that cause an item to be recreated. If the key
	// of an actual item is not found in the desired list, it is dropped. If
	// the key of a desired item does not exit, it is created.
	poolKey := func(p *cloudscale.LoadBalancerPool) string {
		return fmt.Sprint(
			p.Name,
			p.Algorithm,
			p.Protocol,
		)
	}

	poolMemberKey := func(m cloudscale.LoadBalancerPoolMember) string {
		return fmt.Sprintf(
			m.Name,
			m.Enabled,
			m.MonitorPort,
			m.ProtocolPort,
			m.Address,
			m.Subnet,
		)
	}

	listenerKey := func(l cloudscale.LoadBalancerListener) string {
		return fmt.Sprintf(
			l.Name,
			l.Protocol,
			l.ProtocolPort,
		)
	}

	monitorKey := func(m cloudscale.LoadBalancerHealthMonitor) string {
		return fmt.Sprintf(
			m.Type,
		)
	}

	// If no lb is desired, and there is none, stop
	if desired.lb == nil && actual.lb == nil {
		return next, nil
	}

	// If an lb is desired, and there is none, create one. This always causes
	// a re-evaluation and we'll be called again with an existing lb.
	if desired.lb != nil && actual.lb == nil {
		next = append(next,
			actions.CreateLb(desired.lb),
			actions.Refetch(),
		)

		return next, nil
	}

	// No matter what happens next, we need an lb that is ready
	next = append(next, actions.AwaitLb(actual.lb))

	// If the lb should be deleted, do so (causes a cascade)
	if desired.lb == nil && actual.lb != nil {
		next = append(next, actions.DeleteResource(actual.lb.HREF))
		return next, nil
	}

	// If the lb requires other changes, inform the user that they need to
	// recreate the service themselves.
	if len(desired.lb.VIPAddresses) > 0 {
		if !slices.Equal(desired.lb.VIPAddresses, actual.lb.VIPAddresses) {
			return nil, fmt.Errorf(
				"VIP addresses for %s changed, please re-create the service",
				actual.lb.HREF,
			)
		}
	}

	if desired.lb.Flavor.Slug != actual.lb.Flavor.Slug {
		return nil, fmt.Errorf(
			"flavor for %s changed, please configure the previous flavor "+
				"or contact support",
			actual.lb.HREF,
		)
	}

	if desired.lb.Zone.Slug != actual.lb.Zone.Slug {
		return nil, fmt.Errorf(
			"zone for %s changed, please configure the previous zone"+
				"or contact support",
			actual.lb.HREF,
		)
	}

	// If the name of the lb is wrong, change it
	if desired.lb.Name != actual.lb.Name {
		next = append(next, actions.RenameLb(actual.lb.UUID, actual.lb.Name))
	}

	// All other changes are applied aggressively, as the customer would have
	// to do that manually anyway by recreating the service, which would be
	// more disruptive.
	poolsToDelete, poolsToCreate := compare.Diff[*cloudscale.LoadBalancerPool](
		desired.pools,
		actual.pools,
		poolKey,
	)

	// Remove undesired pools
	for _, p := range poolsToDelete {
		for _, m := range actual.members[p] {
			delete(m.HREF)
		}
		delete(p.HREF)
	}

	// Create missing pools
	for _, p := range poolsToCreate {
		next = append(next, actions.CreatePool(actual.lb.UUID, p))
	}

	// If there have been pool changes, refresh
	if len(poolsToDelete) > 0 || len(poolsToCreate) > 0 {
		next = append(next, actions.Refetch())
		return next, nil
	}

	// Update pool members
	actualPools := actual.poolsByName()
	actionCount := len(next)

	for _, d := range desired.pools {
		a := actualPools[d.Name]

		// This would indicate a programming error above
		if a == nil {
			return nil, fmt.Errorf("no existing pool found for %s", d.Name)
		}

		// Delete and create pool members
		msToDelete, msToCreate := compare.Diff(
			desired.members[d],
			actual.members[a],
			poolMemberKey,
		)

		for _, m := range msToDelete {
			member := m
			delete(member.HREF)
		}

		for _, m := range msToCreate {
			member := m
			next = append(next, actions.CreatePoolMember(a.UUID, &member))
		}

		// Delete and create listeners
		lsToDelete, lsToCreate := compare.Diff(
			desired.listeners[d],
			actual.listeners[a],
			listenerKey,
		)

		for _, l := range lsToDelete {
			listener := l
			delete(listener.HREF)
		}

		for _, l := range lsToCreate {
			listener := l
			next = append(next, actions.CreateListener(a.UUID, &listener))
		}

		// Delete and create monitors
		monToDelete, monToCreate := compare.Diff(
			desired.monitors[d],
			actual.monitors[a],
			monitorKey,
		)

		for _, m := range monToDelete {
			mon := m
			delete(mon.HREF)
		}

		for _, m := range monToCreate {
			mon := m
			next = append(next, actions.CreateHealthMonitor(a.UUID, &mon))
		}
	}

	// If there have been member changes, refresh
	if actionCount < len(next) {
		next = append(next, actions.Refetch())
		return next, nil
	}

	// Update the listeners and monitors that do not need to be recreated
	for _, d := range desired.pools {
		a := actualPools[d.Name]

		listeners := compare.Match(
			desired.listeners[d],
			actual.listeners[a],
			listenerKey,
		)

		for _, match := range listeners {
			dl := match[0]
			al := match[1]

			if !slices.Equal(dl.AllowedCIDRs, al.AllowedCIDRs) {
				next = append(next, actions.UpdateListenerAllowedCIDRs(
					al.UUID,
					dl.AllowedCIDRs,
				))
			}

			if dl.TimeoutClientDataMS != al.TimeoutClientDataMS {
				next = append(next, actions.UpdateListenerTimeout(
					al.UUID,
					dl.TimeoutClientDataMS,
					"client-data-ms",
				))
			}

			if dl.TimeoutMemberConnectMS != al.TimeoutMemberConnectMS {
				next = append(next, actions.UpdateListenerTimeout(
					al.UUID,
					dl.TimeoutMemberConnectMS,
					"member-connect-ms",
				))
			}

			if dl.TimeoutMemberDataMS != al.TimeoutMemberDataMS {
				next = append(next, actions.UpdateListenerTimeout(
					al.UUID,
					dl.TimeoutMemberDataMS,
					"member-data-ms",
				))
			}
		}

		monitors := compare.Match(
			desired.monitors[d],
			actual.monitors[a],
			monitorKey,
		)

		for _, match := range monitors {
			dm := match[0]
			am := match[1]

			if !isEqualHTTPOption(dm.HTTP, am.HTTP) {
				next = append(next, actions.UpdateMonitorHTTP(
					am.UUID,
					dm.HTTP,
				))
			}

			if dm.DelayS != am.DelayS {
				next = append(next, actions.UpdateMonitorNumber(
					am.UUID,
					dm.DelayS,
					"delay-s",
				))
			}

			if dm.TimeoutS != am.TimeoutS {
				next = append(next, actions.UpdateMonitorNumber(
					am.UUID,
					dm.TimeoutS,
					"timeout-s",
				))
			}

			if dm.UpThreshold != am.UpThreshold {
				next = append(next, actions.UpdateMonitorNumber(
					am.UUID,
					dm.UpThreshold,
					"up-threshold",
				))
			}

			if dm.DownThreshold != am.DownThreshold {
				next = append(next, actions.UpdateMonitorNumber(
					am.UUID,
					dm.DownThreshold,
					"down-threshold",
				))
			}
		}
	}

	return next, nil
}

// reconcileLbState reconciles an actual load balancer state with a desired
// one. During reconciliation, the state may have to be re-fetche, which is why
// functions are used. They are expected not to cache their results.
func reconcileLbState(
	ctx context.Context,
	client *cloudscale.Client,
	desiredState func() (*lbState, error),
	actualState func() (*lbState, error),
) error {

	for {
		// Get the states
		desired, err := desiredState()
		if err != nil {
			return err
		}

		actual, err := actualState()
		if err != nil {
			return err
		}

		// Get the actions necessary to get to the desired state
		next, err := nextLbActions(desired, actual)
		if err != nil {
			return err
		}

		updateState, err := runActions(ctx, client, next)
		if err != nil {
			return err
		}

		if !updateState {
			break
		}

		// Wait between 5-7.5 seconds between state fetches
		wait := time.Duration(5000+rand.Intn(2500)) * time.Millisecond

		select {
		case <-ctx.Done():
			return fmt.Errorf("action has been aborted")
		case <-time.After(wait):
			continue
		}
	}

	return nil
}

// runActions executes the given actions and returns the result, together
// with a boolean set to true, if additional actions are necessary.
func runActions(
	ctx context.Context,
	client *cloudscale.Client,
	next []actions.Action,
) (bool, error) {

	for _, action := range next {

		// Abort the actions if the context has been cancelled, to avoid
		// noop-ing a bunch of individual function calls.
		if ctx.Err() != nil {
			return false, fmt.Errorf(
				"aborted action run, cancelled: %w", ctx.Err())
		}

		// Execute action and log it
		klog.InfoS("executing action", "label", action.Label())
		control, err := action.Run(ctx, client)

		switch {
		case err != nil:
			return false, fmt.Errorf(
				"error during %s: %w", action.Label(), err)
		case control == actions.Refresh:
			return true, nil
		case control == actions.Proceed:
			continue
		case control == actions.Errored:
			return false, fmt.Errorf("action errored but provided no error")
		default:
			return false, fmt.Errorf("unknown control code: %d", control)
		}
	}

	return false, nil
}

// listenerForPort returns a desired listener for the given port, taking the
// annotations into consideration.
func listenerForPort(
	serviceInfo *serviceInfo,
	port int,
) (*cloudscale.LoadBalancerListener, error) {

	var (
		listener = cloudscale.LoadBalancerListener{}
		err      error
	)

	listener.Protocol = serviceInfo.annotation(LoadBalancerListenerProtocol)
	listener.ProtocolPort = port
	listener.Name = listenerName(listener.Protocol, listener.ProtocolPort)

	listener.TimeoutClientDataMS, err = serviceInfo.annotationInt(
		LoadBalancerListenerTimeoutClientDataMS)
	if err != nil {
		return nil, err
	}

	listener.TimeoutMemberConnectMS, err = serviceInfo.annotationInt(
		LoadBalancerListenerTimeoutMemberConnectMS)
	if err != nil {
		return nil, err
	}

	listener.TimeoutMemberDataMS, err = serviceInfo.annotationInt(
		LoadBalancerListenerTimeoutMemberDataMS)
	if err != nil {
		return nil, err
	}

	listener.AllowedCIDRs, err = serviceInfo.annotationList(
		LoadBalancerListenerAllowedCIDRs)
	if err != nil {
		return nil, err
	}

	return &listener, nil
}

// healthMonitorForPort returns a health monitor for any pool used by the
// given service, taking the annotations into consideration.
func healthMonitorForPort(
	serviceInfo *serviceInfo) (*cloudscale.LoadBalancerHealthMonitor, error) {

	var (
		monitor = cloudscale.LoadBalancerHealthMonitor{}
		err     error
	)

	monitor.Type = serviceInfo.annotation(LoadBalancerHealthMonitorType)

	monitor.DelayS, err = serviceInfo.annotationInt(
		LoadBalancerHealthMonitorDelayS)
	if err != nil {
		return nil, err
	}

	monitor.TimeoutS, err = serviceInfo.annotationInt(
		LoadBalancerHealthMonitorTimeoutS)
	if err != nil {
		return nil, err
	}

	monitor.UpThreshold, err = serviceInfo.annotationInt(
		LoadBalancerHealthMonitorUpThreshold)
	if err != nil {
		return nil, err
	}

	monitor.DownThreshold, err = serviceInfo.annotationInt(
		LoadBalancerHealthMonitorDownThreshold)
	if err != nil {
		return nil, err
	}

	http := serviceInfo.annotation(LoadBalancerHealthMonitorHTTP)
	if http != "{}" {
		err = json.Unmarshal([]byte(http), &monitor.HTTP)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid json in %s: %w",
				LoadBalancerHealthMonitorHTTP,
				err,
			)
		}
	}

	return &monitor, nil
}

// poolsByName returns the pools found in the state, keyed by name
func (l *lbState) poolsByName() map[string]*cloudscale.LoadBalancerPool {
	pools := make(map[string]*cloudscale.LoadBalancerPool, len(l.pools))
	for _, p := range l.pools {
		pools[p.Name] = p
	}
	return pools
}

// isEqualHTTPOption returns true if the two http options are the same
func isEqualHTTPOption(
	a *cloudscale.LoadBalancerHealthMonitorHTTP,
	b *cloudscale.LoadBalancerHealthMonitorHTTP) bool {

	// Pointer comparison
	if a == b {
		return true
	}

	if !slices.Equal(a.ExpectedCodes, b.ExpectedCodes) {
		return false
	}

	if a.Host != b.Host {
		return false
	}

	if a.Method != b.Method {
		return false
	}

	if a.UrlPath != b.UrlPath {
		return false
	}

	if a.Version != b.Version {
		return false
	}

	return true
}

// poolName produces the name of the pool for the given service port (the port
// that is bound on the load balancer and reachable from outside of it).
//
// Warning: This named is used to compare desired pools to actual pools.
// Any change to it causes pools to be rebuilt, which must be avoided!
func poolName(protocol v1.Protocol, port int32) string {
	return strings.ToLower(fmt.Sprintf("%s/%d", protocol, port))
}

// poolMemberName produces the name of the pool member for the given node
// and port. This refers to the socket bound on the node, which receives
// traffic from the loadbalancer.
//
// Warning: This named is used to compare desired members to actual members.
// Any change to it causes members to be rebuilt, which must be avoided!
func poolMemberName(address string, port int) string {

	// Use canonical IPv6 formatting
	if strings.Contains(address, ":") {
		address = fmt.Sprintf("[%s]", address)
	}

	return fmt.Sprintf("%s:%d", address, port)
}

// listenerName produces the name of the listener for the given protocol
// and port. This is similar to the pool name, but here we use values that
// cloudscale API handles, not Kubernetes.
//
// Warning: This named is used to compare desired listeners to actual
// listeners. Any change to it causes members to be rebuilt, which must be
// avoided!
func listenerName(protocol string, port int) string {
	return strings.ToLower(fmt.Sprintf("%s/%d", protocol, port))
}
