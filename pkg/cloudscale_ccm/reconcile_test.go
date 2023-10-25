package cloudscale_ccm

import (
	"context"
	"testing"
	"time"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/actions"
	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	"github.com/cloudscale-ch/cloudscale-go-sdk/v3"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

func TestPoolName(t *testing.T) {
	assert.Equal(t, "tcp/80", poolName(v1.ProtocolTCP, 80))
	assert.Equal(t, "udp/443", poolName(v1.ProtocolUDP, 443))
}

func TestPoolMemberName(t *testing.T) {
	assert.Equal(t, "10.0.0.1:80", poolMemberName("10.0.0.1", 80))
	assert.Equal(t, "[::1]:443", poolMemberName("::1", 443))
}

func TestListenerName(t *testing.T) {
	assert.Equal(t, "tcp/80", listenerName("TCP", 80))
	assert.Equal(t, "tcp/443", listenerName("tcp", 443))
}

func TestDesiredName(t *testing.T) {
	s := testkit.NewService("service").V1()
	s.UID = "deadbeef"

	i := newServiceInfo(s, "")

	nodes := []*v1.Node{}
	servers := []cloudscale.Server{}

	// No name is given, generate one
	state, err := desiredLbState(i, nodes, servers)
	assert.NoError(t, err)
	assert.Equal(t, state.lb.Name, "k8s-service-deadbeef")

	// This can be overridden
	s.Annotations = make(map[string]string)
	s.Annotations[LoadBalancerName] = "foo"

	state, err = desiredLbState(i, nodes, servers)
	assert.NoError(t, err)
	assert.Equal(t, state.lb.Name, "foo")
}

func TestDesiredZone(t *testing.T) {
	s := testkit.NewService("service").V1()
	i := newServiceInfo(s, "")

	nodes := []*v1.Node{
		testkit.NewNode("foo").V1(),
		testkit.NewNode("bar").V1(),
	}

	servers := []cloudscale.Server{
		{Name: "foo", ZonalResource: cloudscale.ZonalResource{
			Zone: cloudscale.Zone{Slug: "lpg1"},
		}},
		{Name: "bar", ZonalResource: cloudscale.ZonalResource{
			Zone: cloudscale.Zone{Slug: "rma1"},
		}},
	}

	// Nodes are in different zones, so it's unclear where to put the lb
	_, err := desiredLbState(i, nodes, servers)
	assert.Error(t, err)

	// Once a zone is given, it is clear
	s.Annotations = make(map[string]string)
	s.Annotations[LoadBalancerZone] = "rma1"

	state, err := desiredLbState(i, nodes, servers)
	assert.NoError(t, err)
	assert.Equal(t, "rma1", state.lb.Zone.Slug)
}

func TestDesiredService(t *testing.T) {
	s := testkit.NewService("service").V1()
	i := newServiceInfo(s, "")

	nodes := []*v1.Node{
		testkit.NewNode("worker-1").V1(),
		testkit.NewNode("worker-2").V1(),
	}

	servers := []cloudscale.Server{
		{
			Name: "worker-1",
			ZonalResource: cloudscale.ZonalResource{
				Zone: cloudscale.Zone{Slug: "rma1"},
			},
			Interfaces: []cloudscale.Interface{{
				Addresses: []cloudscale.Address{{
					Address: "10.0.0.1",
					Subnet: cloudscale.SubnetStub{
						UUID: "00000000-0000-0000-0000-000000000000",
					},
				}},
			}},
		},
		{
			Name: "worker-2",
			ZonalResource: cloudscale.ZonalResource{
				Zone: cloudscale.Zone{Slug: "rma1"},
			},
			Interfaces: []cloudscale.Interface{{
				Addresses: []cloudscale.Address{{
					Address: "10.0.0.2",
					Subnet: cloudscale.SubnetStub{
						UUID: "00000000-0000-0000-0000-000000000000",
					},
				}},
			}},
		},
	}

	s.Spec.Ports = []v1.ServicePort{
		{
			Protocol: "TCP",
			Port:     80,
			NodePort: 8080,
		},
		{
			Protocol: "TCP",
			Port:     443,
			NodePort: 8443,
		},
	}

	desired, err := desiredLbState(i, nodes, servers)
	assert.NoError(t, err)

	// Ensure the lb exists
	assert.Equal(t, "lb-standard", desired.lb.Flavor.Slug)
	assert.Len(t, desired.lb.VIPAddresses, 0)

	// Have one pool per service port
	assert.Len(t, desired.pools, 2)
	assert.Equal(t, desired.pools[0].Name, "tcp/80")
	assert.Equal(t, desired.pools[0].Protocol, "tcp")
	assert.Equal(t, desired.pools[0].Algorithm, "round_robin")
	assert.Equal(t, desired.pools[1].Name, "tcp/443")
	assert.Equal(t, desired.pools[0].Protocol, "tcp")
	assert.Equal(t, desired.pools[0].Algorithm, "round_robin")

	// One member per server
	for _, pool := range desired.pools {
		members := desired.members[pool]
		assert.Len(t, members, 2)

		assert.Equal(t, "10.0.0.1", members[0].Address)
		assert.Equal(t, "10.0.0.2", members[1].Address)

		assert.True(t,
			members[0].ProtocolPort == 8443 ||
				members[0].ProtocolPort == 8080)
		assert.True(t,
			members[1].ProtocolPort == 8443 ||
				members[1].ProtocolPort == 8080)
	}

	// One listener per pool
	for _, pool := range desired.pools {
		listeners := desired.listeners[pool]
		assert.Len(t, listeners, 1)

		assert.Equal(t, "tcp", listeners[0].Protocol)
		assert.True(t,
			listeners[0].ProtocolPort == 80 ||
				listeners[0].ProtocolPort == 443)
	}

	// One health monitor per pool
	for _, pool := range desired.pools {
		monitors := desired.monitors[pool]
		assert.Len(t, monitors, 1)
		assert.Equal(t, "tcp", monitors[0].Type)
	}
}

func TestActualState(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.WithLoadBalancers([]cloudscale.LoadBalancer{
		{
			UUID: "00000000-0000-0000-0000-000000000000",
			Name: "k8test-service-test",
		},
	})
	server.On("/v1/load-balancers/pools", 200, []cloudscale.LoadBalancerPool{
		{
			Name: "tcp/80",
			UUID: "00000000-0000-0000-0000-000000000001",
			LoadBalancer: cloudscale.LoadBalancerStub{
				UUID: "00000000-0000-0000-0000-000000000000",
			},
		},
	})
	server.On("/v1/load-balancers/pools/00000000-0000-0000-0000-000000000001"+
		"/members", 200, []cloudscale.LoadBalancerPoolMember{
		{
			Name: "10.0.0.1:8080",
			Pool: cloudscale.LoadBalancerPoolStub{
				UUID: "00000000-0000-0000-0000-000000000001",
			},
		},
	})
	server.On("/v1/load-balancers/listeners", 200,
		[]cloudscale.LoadBalancerListener{
			{
				Name: "tcp/80",
				Pool: cloudscale.LoadBalancerPoolStub{
					UUID: "00000000-0000-0000-0000-000000000001",
				},
			},
		},
	)
	server.On("/v1/load-balancers/health-monitors", 200,
		[]cloudscale.LoadBalancerHealthMonitor{
			{
				Type: "tcp",
				Pool: cloudscale.LoadBalancerPoolStub{
					UUID: "00000000-0000-0000-0000-000000000001",
				},
			},
		},
	)
	server.Start()
	defer server.Close()

	mapper := lbMapper{client: server.Client()}

	s := testkit.NewService("service").V1()
	s.Annotations = make(map[string]string)
	s.Annotations[LoadBalancerUUID] = "00000000-0000-0000-0000-000000000000"

	i := newServiceInfo(s, "")

	actual, err := actualLbState(context.Background(), &mapper, i)
	assert.NoError(t, err)

	assert.Equal(t, "k8test-service-test", actual.lb.Name)
	assert.Len(t, actual.pools, 1)
	assert.Len(t, actual.members, 1)
	assert.Len(t, actual.listeners, 1)
	assert.Len(t, actual.monitors, 1)

	p := actual.pools[0]
	assert.Equal(t, "tcp/80", p.Name)
	assert.Equal(t, "10.0.0.1:8080", actual.members[p][0].Name)
	assert.Equal(t, "tcp/80", actual.listeners[p][0].Name)
	assert.Equal(t, "tcp", actual.monitors[p][0].Type)
}

func TestNextLbActionsInvalidCalls(t *testing.T) {
	assertError := func(d *lbState, a *lbState) {
		_, err := nextLbActions(d, a)
		assert.Error(t, err)
	}

	assertError(nil, nil)
	assertError(nil, &lbState{})
	assertError(&lbState{}, nil)
}

// TestNextLbProhibitDangerousChanges ensures that changes that would cause
// a loadbalancer to be recreated (potentially losing its automatically
// assigned IP addresss) are prohibited.
//
// Such actions can still be done by manually recreating the service in
// Kubernetes. Some actions can be supported in the future, but this should
// be done cautiously.
//
// Changes "inside" the load balancer can be done more aggressively, as all
// states can be recreated. We cannot however, regain a previously IP address,
// assigned automatically.
func TestNextLbProhibitDangerousChanges(t *testing.T) {
	assertError := func(d *lbState, a *lbState) {
		_, err := nextLbActions(d, a)
		assert.Error(t, err)
	}

	// No automatic change of flavor (to be implemented in the future)
	one := &cloudscale.LoadBalancer{
		Name:   "foo",
		Flavor: cloudscale.LoadBalancerFlavorStub{Slug: "lb-standard"},
	}
	two := &cloudscale.LoadBalancer{
		Name:   "bar",
		Flavor: cloudscale.LoadBalancerFlavorStub{Slug: "lb-large"},
	}

	assertError(&lbState{lb: one}, &lbState{lb: two})

	// No automatic change of VIP addresses
	one = &cloudscale.LoadBalancer{
		Name: "foo",
		VIPAddresses: []cloudscale.VIPAddress{
			{Address: "10.0.0.1"},
		},
	}
	two = &cloudscale.LoadBalancer{
		Name: "bar",
		VIPAddresses: []cloudscale.VIPAddress{
			{Address: "10.0.0.2"},
		},
	}

	assertError(&lbState{lb: one}, &lbState{lb: two})

	// No automatic change of zone
	one = &cloudscale.LoadBalancer{
		Name: "foo",
		ZonalResource: cloudscale.ZonalResource{
			Zone: cloudscale.Zone{Slug: "lpg1"},
		},
	}
	two = &cloudscale.LoadBalancer{
		Name: "bar",
		ZonalResource: cloudscale.ZonalResource{
			Zone: cloudscale.Zone{Slug: "rma1"},
		},
	}

	assertError(&lbState{lb: one}, &lbState{lb: two})
}

func TestNextLbActions(t *testing.T) {

	assertActions := func(d *lbState, a *lbState, expected []actions.Action) {
		actions, err := nextLbActions(d, a)
		assert.NoError(t, err)
		assert.Equal(t, expected, actions)
	}

	lb := &cloudscale.LoadBalancer{
		HREF: "foo",
	}

	// Noop
	assertActions(&lbState{}, &lbState{}, []actions.Action{})

	// The await action is always there, to ensure we are not working on
	// an LB that cannot be updated.
	lb.Status = "changing"
	assertActions(&lbState{lb: lb}, &lbState{lb: lb}, []actions.Action{
		actions.AwaitLb(lb),
	})

	lb.Status = "ready"
	assertActions(&lbState{lb: lb}, &lbState{lb: lb}, []actions.Action{
		actions.AwaitLb(lb),
	})

	// Delete lb if not desired
	assertActions(&lbState{}, &lbState{lb: lb}, []actions.Action{
		actions.AwaitLb(lb),
		actions.DeleteResource("foo"),
	})

	// Create lb if desired
	assertActions(&lbState{lb: lb}, &lbState{}, []actions.Action{
		actions.CreateLb(lb),
		actions.Refetch(),
	})

	// Rename lb if name changed. This is safe because the lbs have either
	// been acquired by name (in which case both will have the same name),
	// or by UUID through the service annotation.
	one := &cloudscale.LoadBalancer{
		Name: "foo",
	}
	two := &cloudscale.LoadBalancer{
		UUID: "2",
		Name: "bar",
	}
	assertActions(&lbState{lb: one}, &lbState{lb: two}, []actions.Action{
		actions.AwaitLb(two),
		actions.RenameLb("2", "bar"),
	})
}

func TestNextPoolActions(t *testing.T) {

	assertActions := func(d *lbState, a *lbState, expected []actions.Action) {
		actions, err := nextLbActions(d, a)
		assert.NoError(t, err)
		assert.Equal(t, expected, actions)
	}

	lb := &cloudscale.LoadBalancer{
		UUID: "foo",
		HREF: "foo",
	}

	// No change in pools
	desired := []*cloudscale.LoadBalancerPool{
		{HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
	}
	actual := []*cloudscale.LoadBalancerPool{
		{HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
	}

	assertActions(
		&lbState{lb: lb, pools: desired},
		&lbState{lb: lb, pools: actual},
		[]actions.Action{
			actions.AwaitLb(lb),
		},
	)

	// Delete pools that are not wanted
	desired = []*cloudscale.LoadBalancerPool{
		{HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
	}
	actual = []*cloudscale.LoadBalancerPool{
		{HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
		{HREF: "tcp/443", Name: "tcp/443", Algorithm: "round_robin"},
	}

	assertActions(
		&lbState{lb: lb, pools: desired},
		&lbState{lb: lb, pools: actual},
		[]actions.Action{
			actions.AwaitLb(lb),
			actions.DeleteResource("tcp/443"),
			actions.Sleep(500 * time.Millisecond),
			actions.Refetch(),
		},
	)

	// Create pools that do not exist
	desired = []*cloudscale.LoadBalancerPool{
		{HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
	}
	assertActions(
		&lbState{lb: lb, pools: desired},
		&lbState{lb: lb},
		[]actions.Action{
			actions.AwaitLb(lb),
			actions.CreatePool("foo", desired[0]),
			actions.Refetch(),
		},
	)

	// Delete pools that do not match
	desired = []*cloudscale.LoadBalancerPool{
		{HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
		{HREF: "tcp/443", Name: "tcp/443", Algorithm: "round_robin"},
	}
	actual = []*cloudscale.LoadBalancerPool{
		{HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
		{HREF: "tcp/4433", Name: "tcp/4433", Algorithm: "round_robin"},
	}

	assertActions(
		&lbState{lb: lb, pools: desired},
		&lbState{lb: lb, pools: actual},
		[]actions.Action{
			actions.AwaitLb(lb),
			actions.DeleteResource("tcp/4433"),
			actions.Sleep(500 * time.Millisecond),
			actions.CreatePool("foo", desired[1]),
			actions.Refetch(),
		},
	)

	// Recreate pools if details change
	desired = []*cloudscale.LoadBalancerPool{
		{HREF: "tcp/80", Name: "tcp/80", Algorithm: "source_ip"},
	}
	actual = []*cloudscale.LoadBalancerPool{
		{HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
	}

	assertActions(
		&lbState{lb: lb, pools: desired},
		&lbState{lb: lb, pools: actual},
		[]actions.Action{
			actions.AwaitLb(lb),
			actions.DeleteResource("tcp/80"),
			actions.Sleep(500 * time.Millisecond),
			actions.CreatePool("foo", desired[0]),
			actions.Refetch(),
		},
	)
}

func TestNextPoolMemberActions(t *testing.T) {

	assertActions := func(d *lbState, a *lbState, expected []actions.Action) {
		actions, err := nextLbActions(d, a)
		assert.NoError(t, err)
		assert.Equal(t, expected, actions)
	}

	lb := &cloudscale.LoadBalancer{
		UUID: "foo",
		HREF: "foo",
	}

	desired := newLbState(lb)

	desired.pools = []*cloudscale.LoadBalancerPool{
		{UUID: "1", HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
	}

	actual := newLbState(desired.lb)
	actual.pools = desired.pools

	pool := desired.pools[0]

	// Create pool members
	desired.members[pool] = []cloudscale.LoadBalancerPoolMember{
		{Address: "10.0.0.1", ProtocolPort: 10000},
	}
	actual.members[pool] = []cloudscale.LoadBalancerPoolMember{}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.CreatePoolMember("1", &desired.members[pool][0]),
		actions.Refetch(),
	})

	// Delete pool members
	desired.members[pool] = []cloudscale.LoadBalancerPoolMember{}
	actual.members[pool] = []cloudscale.LoadBalancerPoolMember{
		{HREF: "10.0.0.1:10000", Address: "10.0.0.1", ProtocolPort: 10000},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.DeleteResource("10.0.0.1:10000"),
		actions.Sleep(500 * time.Millisecond),
		actions.Refetch(),
	})

	// Recreate pool members
	desired.members[pool] = []cloudscale.LoadBalancerPoolMember{
		{Address: "10.0.0.1", ProtocolPort: 2},
	}
	actual.members[pool] = []cloudscale.LoadBalancerPoolMember{
		{HREF: "actual", Address: "10.0.0.1", ProtocolPort: 1},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.DeleteResource("actual"),
		actions.Sleep(500 * time.Millisecond),
		actions.Sleep(5000 * time.Millisecond),
		actions.CreatePoolMember("1", &desired.members[pool][0]),
		actions.Refetch(),
	})
}

func TestNextListenerActions(t *testing.T) {

	assertActions := func(d *lbState, a *lbState, expected []actions.Action) {
		actions, err := nextLbActions(d, a)
		assert.NoError(t, err)
		assert.Equal(t, expected, actions)
	}

	lb := &cloudscale.LoadBalancer{
		UUID: "foo",
		HREF: "foo",
	}

	desired := newLbState(lb)

	desired.pools = []*cloudscale.LoadBalancerPool{
		{UUID: "1", HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
	}

	actual := newLbState(desired.lb)
	actual.pools = desired.pools

	pool := desired.pools[0]

	// Create listeners
	desired.listeners[pool] = []cloudscale.LoadBalancerListener{
		{Name: "tcp/80", ProtocolPort: 80},
	}
	actual.listeners[pool] = []cloudscale.LoadBalancerListener{}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.CreateListener("1", &desired.listeners[pool][0]),
		actions.Refetch(),
	})

	// Delete listeners
	desired.listeners[pool] = []cloudscale.LoadBalancerListener{}
	actual.listeners[pool] = []cloudscale.LoadBalancerListener{
		{HREF: "tcp/80", Name: "tcp/80", ProtocolPort: 80},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.DeleteResource("tcp/80"),
		actions.Sleep(500 * time.Millisecond),
		actions.Refetch(),
	})

	// Recreate listeners
	desired.listeners[pool] = []cloudscale.LoadBalancerListener{
		{HREF: "80", Name: "80", Protocol: "tcp"},
	}
	actual.listeners[pool] = []cloudscale.LoadBalancerListener{
		{HREF: "80", Name: "80", Protocol: "udp"},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.DeleteResource("80"),
		actions.Sleep(500 * time.Millisecond),
		actions.CreateListener("1", &desired.listeners[pool][0]),
		actions.Refetch(),
	})

	// Update allowed CIDRs
	desired.listeners[pool] = []cloudscale.LoadBalancerListener{
		{UUID: "1", HREF: "tcp/80", Name: "tcp/80", ProtocolPort: 80,
			AllowedCIDRs: []string{"7.0.0.0/8"}},
	}
	actual.listeners[pool] = []cloudscale.LoadBalancerListener{
		{UUID: "1", HREF: "tcp/80", Name: "tcp/80", ProtocolPort: 80,
			AllowedCIDRs: []string{}},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.UpdateListenerAllowedCIDRs("1", []string{"7.0.0.0/8"}),
	})

	// Update timeouts
	desired.listeners[pool] = []cloudscale.LoadBalancerListener{
		{UUID: "1", HREF: "tcp/80", Name: "tcp/80", ProtocolPort: 80,
			TimeoutClientDataMS:    1,
			TimeoutMemberConnectMS: 2,
			TimeoutMemberDataMS:    3,
		},
	}
	actual.listeners[pool] = []cloudscale.LoadBalancerListener{
		{UUID: "1", HREF: "tcp/80", Name: "tcp/80", ProtocolPort: 80,
			TimeoutClientDataMS:    3,
			TimeoutMemberConnectMS: 2,
			TimeoutMemberDataMS:    1,
		},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.UpdateListenerTimeout("1", 1, "client-data-ms"),
		actions.UpdateListenerTimeout("1", 3, "member-data-ms"),
	})

}

func TestNextMonitorActions(t *testing.T) {

	assertActions := func(d *lbState, a *lbState, expected []actions.Action) {
		actions, err := nextLbActions(d, a)
		assert.NoError(t, err)
		assert.Equal(t, expected, actions)
	}

	lb := &cloudscale.LoadBalancer{
		UUID: "foo",
		HREF: "foo",
	}

	desired := newLbState(lb)

	desired.pools = []*cloudscale.LoadBalancerPool{
		{UUID: "1", HREF: "tcp/80", Name: "tcp/80", Algorithm: "round_robin"},
	}

	actual := newLbState(desired.lb)
	actual.pools = desired.pools

	pool := desired.pools[0]

	// Create monitors
	desired.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{Type: "tcp"},
	}
	actual.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.CreateHealthMonitor("1", &desired.monitors[pool][0]),
		actions.Refetch(),
	})

	// Delete monitors
	desired.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{}
	actual.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{HREF: "tcp", Type: "tcp"},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.DeleteResource("tcp"),
		actions.Sleep(500 * time.Millisecond),
		actions.Refetch(),
	})

	// Recreate monitors
	desired.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{Type: "http"},
	}
	actual.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{HREF: "tcp", Type: "tcp"},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.DeleteResource("tcp"),
		actions.Sleep(500 * time.Millisecond),
		actions.CreateHealthMonitor("1", &desired.monitors[pool][0]),
		actions.Refetch(),
	})

	// Update http options (no change)
	desired.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{Type: "http", HTTP: &cloudscale.LoadBalancerHealthMonitorHTTP{
			Method: "HEAD",
		}},
	}
	actual.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{Type: "http", HTTP: &cloudscale.LoadBalancerHealthMonitorHTTP{
			Method: "HEAD",
		}},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
	})

	// Update http options
	desired.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{HTTP: &cloudscale.LoadBalancerHealthMonitorHTTP{
			Method: "HEAD",
		}},
	}
	actual.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{UUID: "1", HTTP: &cloudscale.LoadBalancerHealthMonitorHTTP{
			Method: "GET",
		}},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.UpdateMonitorHTTP("1", desired.monitors[pool][0].HTTP),
	})

	// Update monitor numbers
	desired.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{
			DelayS:        1,
			TimeoutS:      2,
			UpThreshold:   3,
			DownThreshold: 4,
		},
	}
	actual.monitors[pool] = []cloudscale.LoadBalancerHealthMonitor{
		{
			UUID:          "1",
			DelayS:        4,
			TimeoutS:      3,
			UpThreshold:   2,
			DownThreshold: 4,
		},
	}

	assertActions(desired, actual, []actions.Action{
		actions.AwaitLb(lb),
		actions.UpdateMonitorNumber("1", 1, "delay-s"),
		actions.UpdateMonitorNumber("1", 2, "timeout-s"),
		actions.UpdateMonitorNumber("1", 3, "up-threshold"),
	})
}
