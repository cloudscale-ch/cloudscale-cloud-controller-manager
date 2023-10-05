package cloudscale_ccm

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/limiter"
	"github.com/cloudscale-ch/cloudscale-go-sdk/v3"
	v1 "k8s.io/api/core/v1"
)

// serverMapper maps cloudscale servers to Kubernetes nodes.
type serverMapper struct {
	client *cloudscale.Client
}

// findByNode finds a server by node name, or requests it by provider id.
func (s *serverMapper) findByNode(
	ctx context.Context,
	node *v1.Node,
) *limiter.Limiter[cloudscale.Server] {

	if node == nil {
		return limiter.New[cloudscale.Server](nil)
	}

	if node.Spec.ProviderID != "" {
		providerID, err := parseCloudscaleProviderID(node.Spec.ProviderID)

		// If there *is* a provider ID, but it is not valid, we return an
		// error, as we can't say that the instance exist or not (it could
		// be from another cloud provider and we have no knowledge about
		// them).
		//
		// See also https://github.com/kubernetes/cloud-provider/issues/3
		if err != nil {
			return limiter.New[cloudscale.Server](fmt.Errorf(
				"%s is not a valid cloudscale provider id: %w",
				node.Spec.ProviderID,
				err,
			))
		}

		return s.getByProviderID(ctx, *providerID)
	}

	return s.findByName(ctx, node.Name)
}

// mapNodes returns a server for each given node. If a 1:1 mapping across all
// given nodes can be established, an error is returned.
func (s *serverMapper) mapNodes(
	ctx context.Context,
	nodes []*v1.Node,
) *limiter.Limiter[cloudscale.Server] {
	servers := make([]cloudscale.Server, 0, len(nodes))

	for _, node := range nodes {
		server, err := s.findByNode(ctx, node).One()

		if err != nil {
			return limiter.New[cloudscale.Server](err)
		}

		servers = append(servers, *server)
	}

	return limiter.New[cloudscale.Server](nil, servers...)
}

// getByProviderID tries to access the server by provider ID (UUID)
func (s *serverMapper) getByProviderID(
	ctx context.Context,
	id cloudscaleProviderID,
) *limiter.Limiter[cloudscale.Server] {

	server, err := s.client.Servers.Get(ctx, id.UUID().String())
	if err != nil {
		var response *cloudscale.ErrorResponse

		if errors.As(err, &response) && response.StatusCode == 404 {
			return limiter.New[cloudscale.Server](nil)
		}

		return limiter.New[cloudscale.Server](err)
	}

	return limiter.New[cloudscale.Server](nil, *server)
}

// findByName returns servers matching the given name (there may be multiple
// matches).
func (s *serverMapper) findByName(
	ctx context.Context,
	name string,
) *limiter.Limiter[cloudscale.Server] {

	servers, err := s.client.Servers.List(ctx)
	if err != nil {
		return limiter.New[cloudscale.Server](err)
	}

	matches := []cloudscale.Server{}
	for _, server := range servers {
		srv := server

		if srv.Name == name {
			matches = append(matches, srv)
		}
	}

	return limiter.New[cloudscale.Server](nil, matches...)
}

// nodeAddresses returns a v1.nodeAddresses slice for the metadata
func (s *serverMapper) nodeAddresses(
	server *cloudscale.Server) []v1.NodeAddress {

	if server == nil {
		return []v1.NodeAddress{}
	}

	// We're likely going to have three entries (hostname, ipv4, ipv6), so
	// use that as the initial capacity.
	addrs := make([]v1.NodeAddress, 0, 3)

	addrs = append(addrs, v1.NodeAddress{
		Type:    v1.NodeHostName,
		Address: server.Name,
	})

	for _, i := range server.Interfaces {
		for _, a := range i.Addresses {
			var addr v1.NodeAddress

			switch i.Type {
			case "public":
				addr = v1.NodeAddress{
					Type:    v1.NodeExternalIP,
					Address: a.Address,
				}
			default:
				addr = v1.NodeAddress{
					Type:    v1.NodeInternalIP,
					Address: a.Address,
				}
			}

			addrs = append(addrs, addr)
		}
	}

	return addrs
}
