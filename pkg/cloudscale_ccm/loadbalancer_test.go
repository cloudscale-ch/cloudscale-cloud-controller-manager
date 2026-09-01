package cloudscale_ccm

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
)

func TestLoadBalancer_EnsureLoadBalancer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		annotations map[string]string
		nodes       []*v1.Node
		setup       func(*testkit.MockAPIServer)
		wantErr     bool
		wantErrMsg  string
		wantEvent   string
	}{
		{
			name: "invalid node selector returns error",
			annotations: map[string]string{
				LoadBalancerNodeSelector: "invalid===syntax",
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
			},
			setup:      func(apiServer *testkit.MockAPIServer) {},
			wantErr:    true,
			wantErrMsg: "unable to parse selector",
		},
		{
			name: "node selector matching no nodes emits warning event",
			annotations: map[string]string{
				LoadBalancerNodeSelector: "nonexistent=value",
				LoadBalancerName:         "test-lb",
				LoadBalancerFlavor:       "lb-standard",
				LoadBalancerZone:         "rma1",
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"env": "prod"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"env": "staging"}}},
			},
			setup: func(apiServer *testkit.MockAPIServer) {
				lbUUID := "00000000-0000-0000-0000-000000000001"
				poolUUID := "00000000-0000-0000-0000-000000000002"
				listenerUUID := "00000000-0000-0000-0000-000000000003"
				monitorUUID := "00000000-0000-0000-0000-000000000004"

				apiServer.WithLoadBalancers([]cloudscale.LoadBalancer{{
					HREF:   "/v1/load-balancers/" + lbUUID,
					UUID:   lbUUID,
					Name:   "test-lb",
					Status: "running",
					ZonalResource: cloudscale.ZonalResource{
						Zone: cloudscale.Zone{Slug: "rma1"},
					},
					Flavor: cloudscale.LoadBalancerFlavorStub{Slug: "lb-standard"},
				}})
				apiServer.On("/v1/load-balancers/pools", 200, []cloudscale.LoadBalancerPool{{
					HREF:         "/v1/load-balancers/pools/" + poolUUID,
					UUID:         poolUUID,
					Name:         "tcp",
					LoadBalancer: cloudscale.LoadBalancerStub{UUID: lbUUID},
					Algorithm:    "round_robin",
					Protocol:     "tcp",
				}})
				apiServer.On("/v1/load-balancers/pools/"+poolUUID+"/members", 200,
					[]cloudscale.LoadBalancerPoolMember{})
				apiServer.On("/v1/load-balancers/listeners", 200, []cloudscale.LoadBalancerListener{{
					HREF:                   "/v1/load-balancers/listeners/" + listenerUUID,
					UUID:                   listenerUUID,
					Name:                   "tcp/80",
					Pool:                   &cloudscale.LoadBalancerPoolStub{UUID: poolUUID},
					LoadBalancer:           cloudscale.LoadBalancerStub{UUID: lbUUID},
					Protocol:               "tcp",
					ProtocolPort:           80,
					TimeoutClientDataMS:    50000,
					TimeoutMemberConnectMS: 5000,
					TimeoutMemberDataMS:    50000,
					AllowedCIDRs:           []string{},
				}})
				apiServer.On("/v1/load-balancers/health-monitors", 200, []cloudscale.LoadBalancerHealthMonitor{{
					HREF:          "/v1/load-balancers/health-monitors/" + monitorUUID,
					UUID:          monitorUUID,
					Pool:          cloudscale.LoadBalancerPoolStub{UUID: poolUUID},
					Type:          "tcp",
					DelayS:        2,
					TimeoutS:      1,
					UpThreshold:   2,
					DownThreshold: 3,
				}})
				apiServer.On("/v1/floating-ips", 200, []cloudscale.FloatingIP{})
			},
			wantErr:   false,
			wantEvent: "NoValidNodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiServer := testkit.NewMockAPIServer()
			if tt.setup != nil {
				tt.setup(apiServer)
			}
			apiServer.Start()
			defer apiServer.Close()

			fr := record.NewFakeRecorder(10)
			client := fake.NewSimpleClientset()

			// ensure `kubeutil.IsKubernetesReleaseOrNewer` works by faking a server version which is usually not set
			// when using fake client.
			fakeDiscovery, ok := client.Discovery().(*fakediscovery.FakeDiscovery)
			require.True(t, ok, "couldn't convert Discovery() to *FakeDiscovery")

			fakeDiscovery.FakedServerVersion = &version.Info{
				Major: "1",
				Minor: "34",
			}

			l := &loadbalancer{
				lbs:      lbMapper{client: apiServer.Client()},
				srv:      serverMapper{client: apiServer.Client()},
				k8s:      client,
				recorder: fr,
			}

			service := &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-service",
					Namespace:   "default",
					UID:         "test-uid",
					Annotations: tt.annotations,
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
					Ports: []v1.ServicePort{
						{Protocol: v1.ProtocolTCP, Port: 80, NodePort: 80},
					},
				},
			}

			// Pre-create the service in the fake k8s client so annotations can be applied
			_, _ = l.k8s.CoreV1().Services("default").Create(t.Context(), service, metav1.CreateOptions{})

			_, err := l.EnsureLoadBalancer(t.Context(), "test-cluster", service, tt.nodes)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
			} else {
				assert.NoError(t, err)
			}

			if tt.wantEvent != "" {
				select {
				case event := <-fr.Events:
					assert.Contains(t, event, tt.wantEvent)
				default:
					t.Error("expected warning event but none was recorded")
				}
			}
		})
	}
}

func TestLoadBalancer_ConcurrentCreate(t *testing.T) {
	t.Parallel()

	apiServer := testkit.NewMockAPIServer()

	createCount := 0
	var lbs []cloudscale.LoadBalancer
	var mu sync.Mutex

	// Custom handler for /v1/load-balancers to track creates.
	// The sleep before appending to lbs increases the race window so that
	// both goroutines can see an empty list before either creates.
	apiServer.HandleFunc("/v1/load-balancers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			time.Sleep(200 * time.Millisecond)

			mu.Lock()
			createCount++
			lb := cloudscale.LoadBalancer{
				HREF:   "/v1/load-balancers/lb-uuid-1",
				UUID:   "lb-uuid-1",
				Name:   "k8s-service-test-uid",
				Status: "running",
				ZonalResource: cloudscale.ZonalResource{
					Zone: cloudscale.Zone{Slug: "rma1"},
				},
				Flavor: cloudscale.LoadBalancerFlavorStub{Slug: "lb-standard"},
			}
			lbs = append(lbs, lb)
			mu.Unlock()

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(lb)
		case http.MethodGet:
			mu.Lock()
			defer mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(lbs)
		}
	})

	// Mock server endpoint for node mapping.
	serverUUID := "08d56bfe-40d0-4c68-a915-54f846c28c9e"
	apiServer.WithServers([]cloudscale.Server{{
		UUID: serverUUID,
		Name: "node-1",
		ZonalResource: cloudscale.ZonalResource{
			Zone: cloudscale.Zone{Slug: "rma1"},
		},
		Interfaces: []cloudscale.Interface{{
			Type: "private",
			Addresses: []cloudscale.Address{{
				Address: "10.0.0.1",
				Subnet:  cloudscale.SubnetStub{UUID: "subnet-uuid-1"},
			}},
		}},
	}})

	// Mock the remaining LB endpoints so reconciliation can proceed.
	apiServer.On("/v1/load-balancers/pools", 200, []cloudscale.LoadBalancerPool{})
	apiServer.On("/v1/load-balancers/listeners", 200, []cloudscale.LoadBalancerListener{})
	apiServer.On("/v1/load-balancers/health-monitors", 200, []cloudscale.LoadBalancerHealthMonitor{})
	apiServer.On("/v1/floating-ips", 200, []cloudscale.FloatingIP{})

	apiServer.Start()
	defer apiServer.Close()

	client := fake.NewSimpleClientset()
	fakeDiscovery, ok := client.Discovery().(*fakediscovery.FakeDiscovery)
	require.True(t, ok, "couldn't convert Discovery() to *FakeDiscovery")
	fakeDiscovery.FakedServerVersion = &version.Info{
		Major: "1",
		Minor: "34",
	}

	l := &loadbalancer{
		lbs:      lbMapper{client: apiServer.Client()},
		srv:      serverMapper{client: apiServer.Client()},
		k8s:      client,
		recorder: record.NewFakeRecorder(10),
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			UID:       "test-uid",
			Annotations: map[string]string{
				LoadBalancerName:   "k8s-service-test-uid",
				LoadBalancerFlavor: "lb-standard",
				LoadBalancerZone:   "rma1",
			},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{Protocol: v1.ProtocolTCP, Port: 80, NodePort: 80},
			},
		},
	}

	_, _ = l.k8s.CoreV1().Services("default").Create(t.Context(), service, metav1.CreateOptions{})

	nodes := []*v1.Node{{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       v1.NodeSpec{ProviderID: "cloudscale://" + serverUUID},
	}}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = l.EnsureLoadBalancer(t.Context(), "test-cluster", service, nodes)
	}()
	go func() {
		defer wg.Done()
		_, _ = l.EnsureLoadBalancer(t.Context(), "test-cluster", service, nodes)
	}()
	wg.Wait()

	assert.Equal(t, 1, createCount, "expected exactly one LB creation")
}

func TestFilterNodesBySelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		annotations map[string]string
		nodes       []*v1.Node
		wantNames   []string
		wantErr     bool
	}{
		{
			name: "selector matches subset of nodes",
			annotations: map[string]string{
				LoadBalancerNodeSelector: "env=prod",
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"env": "prod"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"env": "staging"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-3", Labels: map[string]string{"env": "prod"}}},
			},
			wantNames: []string{"node-1", "node-3"},
			wantErr:   false,
		},
		{
			name: "invalid selector syntax returns error",
			annotations: map[string]string{
				LoadBalancerNodeSelector: "invalid===syntax",
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
			},
			wantNames: nil,
			wantErr:   true,
		},
		{
			name:        "no annotation returns all nodes",
			annotations: nil,
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}},
			},
			wantNames: []string{"node-1", "node-2"},
			wantErr:   false,
		},
		{
			name: "selector matches no nodes returns empty slice",
			annotations: map[string]string{
				LoadBalancerNodeSelector: "nonexistent=value",
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"env": "prod"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"env": "staging"}}},
			},
			wantNames: []string{},
			wantErr:   false,
		},
		{
			name: "selector with multiple requirements",
			annotations: map[string]string{
				LoadBalancerNodeSelector: "env=prod,tier=frontend",
			},
			nodes: []*v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"env": "prod", "tier": "frontend"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"env": "prod", "tier": "backend"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-3", Labels: map[string]string{"env": "staging", "tier": "frontend"}}},
			},
			wantNames: []string{"node-1"},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-service",
					Annotations: tt.annotations,
				},
			}
			info := newServiceInfo(service, "test-cluster")

			got, err := filterNodesBySelector(info, tt.nodes)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "unable to parse selector")

				return
			}
			assert.NoError(t, err)

			gotNames := make([]string, len(got))
			for i, node := range got {
				gotNames[i] = node.Name
			}
			assert.Equal(t, tt.wantNames, gotNames)
		})
	}
}
