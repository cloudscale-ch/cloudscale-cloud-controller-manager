package cloudscale_ccm

import (
	"context"
	"testing"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	"github.com/cloudscale-ch/cloudscale-go-sdk"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

func TestServerByNode(t *testing.T) {
	mapper := staticServerMapper{servers: []cloudscale.Server{
		{UUID: "c2e4aabd-8c91-46da-b069-71e01f439806", Name: "foo"},
		{UUID: "5ac4afba-57b3-40d7-b34a-9da7056176fd", Name: "bar"},
		{UUID: "096c58ff-41c5-44fa-9ba3-05defce2062a", Name: "clone"},
		{UUID: "85dffa20-8097-4d75-afa6-9e4372047ce6", Name: "clone"},
	}}

	assertMatch := func(name string, node *v1.Node) {
		match, err := mapper.byNode(context.Background(), node)
		assert.NoError(t, err)
		assert.Equal(t, name, match.Name)
	}

	assertMissing := func(node *v1.Node) {
		match, err := mapper.byNode(context.Background(), node)
		assert.NoError(t, err)
		assert.Nil(t, match)
	}

	assertError := func(node *v1.Node) {
		_, err := mapper.byNode(context.Background(), node)
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
	mapper := staticServerMapper{servers: []cloudscale.Server{}}

	assertMissing := func(node *v1.Node) {
		match, err := mapper.byNode(context.Background(), node)
		assert.NoError(t, err)
		assert.Nil(t, match)
	}

	// With no servers, everything is missing
	assertMissing(testkit.NewNode("foo").V1())
	assertMissing(testkit.NewNode("").WithProviderID(
		"cloudscale://9a8fa1fc-7fb4-4503-b0d6-b946912a99f1").V1())
}

func TestBadServerMapper(t *testing.T) {
	// This should never happen, but if it does we want an error, not a panic
	mapper := &staticServerMapper{
		servers: []cloudscale.Server{
			{
				UUID: "invalid-uuid-reported-by-api",
				Name: "foo",
			},
		},
	}

	_, err := mapper.byNode(
		context.Background(),
		testkit.NewNode("").WithProviderID(
			"cloudscale://9a8fa1fc-7fb4-4503-b0d6-b946912a99f1").V1(),
	)

	assert.Error(t, err)
}