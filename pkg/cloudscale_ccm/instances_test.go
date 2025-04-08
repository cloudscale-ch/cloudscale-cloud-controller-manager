package cloudscale_ccm

import (
	"testing"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	cloudscale "github.com/cloudscale-ch/cloudscale-go-sdk/v4"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

func TestInstanceExists(t *testing.T) {
	t.Parallel()

	server := testkit.NewMockAPIServer()
	server.WithServers([]cloudscale.Server{
		{UUID: "c2e4aabd-8c91-46da-b069-71e01f439806", Name: "foo"},
		{UUID: "5ac4afba-57b3-40d7-b34a-9da7056176fd", Name: "bar"},
	})
	server.Start()
	defer server.Close()

	i := instances{srv: serverMapper{client: server.Client()}}

	assertExists := func(exists bool, node *v1.Node) {
		actual, err := i.InstanceExists(t.Context(), node)
		assert.NoError(t, err)
		assert.Equal(t, exists, actual)
	}

	assertError := func(node *v1.Node) {
		_, err := i.InstanceExists(t.Context(), node)
		assert.Error(t, err)
	}

	// Only decide if instances exist if they have a provider id
	assertError(testkit.NewNode("foo").V1())
	assertError(testkit.NewNode("bar").V1())
	assertError(testkit.NewNode("baz").V1())

	assertExists(true, testkit.NewNode("baz").WithProviderID(
		"cloudscale://5ac4afba-57b3-40d7-b34a-9da7056176fd").V1())
	assertExists(false, testkit.NewNode("foo").WithProviderID(
		"cloudscale://00000000-0000-0000-0000-000000000000").V1())
}

func TestInstanceShutdown(t *testing.T) {
	t.Parallel()

	server := testkit.NewMockAPIServer()
	server.WithServers([]cloudscale.Server{
		{UUID: "c2e4aabd-8c91-46da-b069-000000000001",
			Name: "foo", Status: "stopped"},
		{UUID: "c2e4aabd-8c91-46da-b069-000000000002",
			Name: "bar", Status: "started"},
		{UUID: "c2e4aabd-8c91-46da-b069-000000000003",
			Name: "baz", Status: "changing"},
	})
	server.Start()
	defer server.Close()

	i := instances{srv: serverMapper{client: server.Client()}}

	assertShutdown := func(shutdown bool, node *v1.Node) {
		actual, err := i.InstanceShutdown(t.Context(), node)
		assert.NoError(t, err)
		assert.Equal(t, shutdown, actual)
	}

	assertError := func(node *v1.Node) {
		_, err := i.InstanceExists(t.Context(), node)
		assert.Error(t, err)
	}

	// Only decide if instances are shut down if they have a provider id
	assertError(testkit.NewNode("foo").V1())
	assertError(testkit.NewNode("bar").V1())
	assertError(testkit.NewNode("baz").V1())

	assertShutdown(true, testkit.NewNode("foo").WithProviderID(
		"cloudscale://c2e4aabd-8c91-46da-b069-000000000001").V1())
	assertShutdown(false, testkit.NewNode("bar").WithProviderID(
		"cloudscale://c2e4aabd-8c91-46da-b069-000000000002").V1())
	assertShutdown(false, testkit.NewNode("baz").WithProviderID(
		"cloudscale://c2e4aabd-8c91-46da-b069-000000000003").V1())

	// If the node cannot be found, we rather err, than make any statement
	// about it being shutdown or not (that's the job of InstanceExists)
	_, err := i.InstanceShutdown(
		t.Context(), testkit.NewNode("unknown").V1())

	assert.Error(t, err)
}

func TestInstanceMetadata(t *testing.T) {
	t.Parallel()

	server := testkit.NewMockAPIServer()
	server.WithServers([]cloudscale.Server{
		{
			UUID: "cdc81195-f37e-46b6-827b-2aa824cfbc82",
			Name: "worker-1",
			Flavor: cloudscale.Flavor{
				Slug: "flex-4-2",
			},
			Interfaces: []cloudscale.Interface{
				{
					Type: "public",
					Addresses: []cloudscale.Address{
						{Address: "5.102.144.1"},
						{Address: "2a06:c01:bb::1"},
					},
				},
				{
					Type: "private",
					Addresses: []cloudscale.Address{
						{Address: "10.0.0.1"},
					},
				},
			},
			ZonalResource: cloudscale.ZonalResource{
				Zone: cloudscale.Zone{Slug: "rma1"},
			},
		},
	})
	server.Start()
	defer server.Close()

	i := instances{srv: serverMapper{client: server.Client()}}

	meta, err := i.InstanceMetadata(
		t.Context(),
		testkit.NewNode("worker-1").V1(),
	)

	assert.NoError(t, err)
	assert.Equal(t,
		"cloudscale://cdc81195-f37e-46b6-827b-2aa824cfbc82",
		meta.ProviderID,
	)
	assert.Equal(t, "flex-4-2", meta.InstanceType)
	assert.Equal(t, "rma1", meta.Zone)
	assert.Equal(t, "rma", meta.Region)
	assert.Equal(t, meta.NodeAddresses, []v1.NodeAddress{
		{Type: v1.NodeHostName, Address: "worker-1"},
		{Type: v1.NodeExternalIP, Address: "5.102.144.1"},
		{Type: v1.NodeExternalIP, Address: "2a06:c01:bb::1"},
		{Type: v1.NodeInternalIP, Address: "10.0.0.1"},
	})
}

func TestInvalidNodeProvider(t *testing.T) {
	t.Parallel()

	server := testkit.NewMockAPIServer()
	server.WithServers([]cloudscale.Server{
		{Name: "foo"},
	})
	server.Start()
	defer server.Close()

	i := instances{srv: serverMapper{client: server.Client()}}

	node := testkit.NewNode("").WithProviderID("cloudscale://invalid").V1()

	_, err := i.InstanceExists(t.Context(), node)
	assert.Error(t, err)

	_, err = i.InstanceShutdown(t.Context(), node)
	assert.Error(t, err)

	_, err = i.InstanceMetadata(t.Context(), node)
	assert.Error(t, err)
}
