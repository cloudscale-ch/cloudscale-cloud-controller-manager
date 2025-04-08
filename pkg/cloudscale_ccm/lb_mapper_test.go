package cloudscale_ccm

import (
	"testing"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	"github.com/cloudscale-ch/cloudscale-go-sdk/v4"
	"github.com/stretchr/testify/assert"
)

func TestFindLoadBalancer(t *testing.T) {
	t.Parallel()

	server := testkit.NewMockAPIServer()
	server.WithLoadBalancers([]cloudscale.LoadBalancer{
		{UUID: "c2e4aabd-8c91-46da-b069-71e01f439806", Name: "foo"},
		{UUID: "096c58ff-41c5-44fa-9ba3-05defce2062a", Name: "clone"},
		{UUID: "85dffa20-8097-4d75-afa6-9e4372047ce6", Name: "clone"},
	})
	server.Start()
	defer server.Close()

	mapper := lbMapper{client: server.Client()}

	s := testkit.NewService("service").V1()
	i := newServiceInfo(s, "")

	// Neither name nor uuid given
	lbs := mapper.findByServiceInfo(t.Context(), i)
	assert.NoError(t, lbs.None())

	// Using a unique name
	s.Annotations = make(map[string]string)
	s.Annotations[LoadBalancerName] = "foo"

	lbs = mapper.findByServiceInfo(t.Context(), i)
	lb, err := lbs.One()
	assert.NoError(t, err)
	assert.Equal(t, "foo", lb.Name)

	// Using an ambiguous name
	s.Annotations[LoadBalancerName] = "clone"

	lbs = mapper.findByServiceInfo(t.Context(), i)
	_, err = lbs.One()
	assert.Error(t, err)

	// Using a uuid
	s.Annotations[LoadBalancerUUID] = "85dffa20-8097-4d75-afa6-9e4372047ce6"

	lbs = mapper.findByServiceInfo(t.Context(), i)
	lb, err = lbs.One()
	assert.NoError(t, err)
	assert.Equal(t, "clone", lb.Name)
}
