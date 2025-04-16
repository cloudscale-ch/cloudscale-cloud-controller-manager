package cloudscale_ccm

import (
	"testing"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	cloudscale "github.com/cloudscale-ch/cloudscale-go-sdk/v6"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

func TestServerByNode(t *testing.T) {
	t.Parallel()

	server := testkit.NewMockAPIServer()
	server.WithServers([]cloudscale.Server{
		{UUID: "c2e4aabd-8c91-46da-b069-71e01f439806", Name: "foo"},
		{UUID: "5ac4afba-57b3-40d7-b34a-9da7056176fd", Name: "bar"},
		{UUID: "096c58ff-41c5-44fa-9ba3-05defce2062a", Name: "clone"},
		{UUID: "85dffa20-8097-4d75-afa6-9e4372047ce6", Name: "clone"},
	})
	server.Start()
	defer server.Close()

	mapper := serverMapper{client: server.Client()}

	assertMatch := func(name string, node *v1.Node) {
		match, err := mapper.findByNode(t.Context(), node).One()
		assert.NoError(t, err)
		assert.Equal(t, name, match.Name)
	}

	assertMissing := func(node *v1.Node) {
		err := mapper.findByNode(t.Context(), node).None()
		assert.NoError(t, err)
	}

	assertError := func(node *v1.Node) {
		_, err := mapper.findByNode(t.Context(), node).One()
		assert.Error(t, err)
	}

	// Select servers by name
	assertMatch("foo", testkit.NewNode("foo").V1())

	// Or by provider id (most accurate)
	assertMatch("bar", testkit.NewNode("").WithProviderID(
		"cloudscale://5ac4afba-57b3-40d7-b34a-9da7056176fd").V1())

	assertMatch("clone", testkit.NewNode("").WithProviderID(
		"cloudscale://096c58ff-41c5-44fa-9ba3-05defce2062a").V1())

	assertMatch("clone", testkit.NewNode("").WithProviderID(
		"cloudscale://85dffa20-8097-4d75-afa6-9e4372047ce6").V1())

	// The provider id has precedence
	assertMatch("foo", testkit.NewNode("bar").WithProviderID(
		"cloudscale://c2e4aabd-8c91-46da-b069-71e01f439806").V1())

	assertMissing(testkit.NewNode("foo").WithProviderID(
		"cloudscale://00000000-0000-0000-0000-000000000000").V1())

	// Err if there's an ambiguous name
	assertError(testkit.NewNode("clone").V1())

	// Err if the input is rubbish
	assertError(testkit.NewNode("foo").WithProviderID("cloudscale://bad").V1())
	assertError(nil)

	// Otherwise the servers are simply missing
	assertMissing(testkit.NewNode("").V1())
	assertMissing(testkit.NewNode("xyz").V1())
	assertMissing(testkit.NewNode("").WithProviderID(
		"cloudscale://9a8fa1fc-7fb4-4503-b0d6-b946912a99f1").V1())
}

func TestNoServers(t *testing.T) {
	t.Parallel()

	server := testkit.NewMockAPIServer()
	server.On("/v1/servers", 200, "[]")
	server.On("/v1/servers/9a8fa1fc-7fb4-4503-b0d6-b946912a99f1", 404, "{}")
	server.Start()
	defer server.Close()

	mapper := serverMapper{client: server.Client()}

	assertMissing := func(node *v1.Node) {
		match, err := mapper.findByNode(t.Context(), node).AtMostOne()
		assert.NoError(t, err)
		assert.Nil(t, match)
	}

	// With no servers, everything is missing
	assertMissing(testkit.NewNode("foo").V1())
	assertMissing(testkit.NewNode("").WithProviderID(
		"cloudscale://9a8fa1fc-7fb4-4503-b0d6-b946912a99f1").V1())
}
