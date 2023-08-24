package cloudscale_ccm

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudscale-ch/cloudscale-go-sdk"
	"github.com/google/uuid"
	v1 "k8s.io/api/core/v1"
)

// serverMapper abstracts the mapping of nodes to servers in a testable way
// where the API call becomes an implementation detail.
type serverMapper interface {
	byNode(ctx context.Context, node *v1.Node) (*cloudscale.Server, error)
}

// apiServerMapper implements server mapping using the cloudscale API
type apiServerMapper struct {
	client *cloudscale.Client
}

func (a *apiServerMapper) byNode(ctx context.Context, node *v1.Node) (
	*cloudscale.Server, error) {
	servers, err := a.client.Servers.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to list servers: %w", err)
	}

	return serverByNode(servers, node)
}

// staticServerMapper implements server mapping using a static server list
type staticServerMapper struct {
	servers []cloudscale.Server
}

func (s *staticServerMapper) byNode(ctx context.Context, node *v1.Node) (
	*cloudscale.Server, error) {
	return serverByNode(s.servers, node)
}

// serverByNode implements the logic to get from a list of servers to a node
func serverByNode(servers []cloudscale.Server, node *v1.Node) (
	*cloudscale.Server, error) {

	if node == nil {
		return nil, fmt.Errorf("node cannot be nil")
	}

	if len(servers) == 0 {
		return nil, nil
	}

	if node.Spec.ProviderID != "" {
		providerID, err := parseCloudscaleProviderID(node.Spec.ProviderID)

		// If there *is* a provider ID, but it is not valid, we return an
		// error, as we can't say that the instance exist or not (it could
		// be from another cloud provider and we have no knowledge about
		// them).
		//
		// See also https://github.com/kubernetes/cloud-provider/issues/35
		if err != nil {
			return nil, fmt.Errorf(
				"%s is not a valid cloudscale provider id: %w",
				node.Spec.ProviderID,
				err,
			)
		}

		return serverByProviderID(servers, *providerID)

	}

	// As fallback, we use name matching, which should work in most cases
	return serverByName(servers, node.Name)
}

func serverByProviderID(servers []cloudscale.Server, id cloudscaleProviderID) (
	*cloudscale.Server, error) {

	for _, server := range servers {
		serverUUID, err := uuid.Parse(server.UUID)

		if err != nil {
			return nil, fmt.Errorf("invalid server UUID: %s", server.UUID)
		}

		if serverUUID == id.UUID() {
			return &server, nil
		}
	}

	return nil, nil
}

func serverByName(servers []cloudscale.Server, name string) (
	*cloudscale.Server, error) {

	var match *cloudscale.Server
	var found int = 0

	for _, server := range servers {
		s := server

		if s.Name == name {
			match = &s
			found++
		}
	}

	if found > 1 {
		return nil, fmt.Errorf("found %d servers named %s", found, name)
	}

	if found == 0 {
		return nil, nil
	}

	return match, nil
}

// serverNodeAddresses returns a v1.NodeAddresses slice for the metadata
func serverNodeAddresses(server *cloudscale.Server) []v1.NodeAddress {
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

// ensureOne takes a byNode answer and ensures that it matches a server
func ensureOne(server *cloudscale.Server, err error) (
	*cloudscale.Server, error) {

	if err != nil {
		return server, err
	}

	if server == nil {
		return nil, errors.New("no matching server found")
	}

	return server, err
}
