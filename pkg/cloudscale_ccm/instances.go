package cloudscale_ccm

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

type instances struct {
	serverMapper serverMapper
}

// InstanceExists returns true if the instance for the given node exists
// according to the cloud provider. Use the node.name or node.spec.providerID
// field to find the node in the cloud provider.
func (i *instances) InstanceExists(ctx context.Context, node *v1.Node) (
	bool, error) {

	server, err := i.serverMapper.byNode(ctx, node)

	if err != nil {
		return false, err
	}

	if server == nil {
		klog.InfoS(
			"instance does not exist",
			"Node", node.Name,
			"ProviderID", node.Spec.ProviderID,
		)
		return false, nil
	}

	klog.InfoS(
		"instance exists",
		"Node", node.Name,
		"ProviderID", node.Spec.ProviderID,
	)
	return true, nil
}

// InstanceShutdown returns true if the instance is shutdown according to the
// cloud provider. Use the node.name or node.spec.providerID field to find the
// node in the cloud provider.
func (i *instances) InstanceShutdown(ctx context.Context, node *v1.Node) (
	bool, error) {

	server, err := ensureOne(i.serverMapper.byNode(ctx, node))

	if err != nil {
		return false, err
	}

	klog.InfoS(
		"instance status",
		"Node", node.Name,
		"ProviderID", node.Spec.ProviderID,
		"Server.Status", server.Status,
	)
	return server.Status == "stopped", nil
}

// InstanceMetadata returns the instance's metadata. The values returned
// in InstanceMetadata are translated into specific fields and labels in
// the Node object on registration. Implementations should always check
// node.spec.providerID first when trying to discover the instance for a given
// node. In cases where node.spec.providerID is empty, implementations can use
// other properties of the node like its name, labels and annotations.
func (i *instances) InstanceMetadata(ctx context.Context, node *v1.Node) (
	*cloudprovider.InstanceMetadata, error) {

	server, err := ensureOne(i.serverMapper.byNode(ctx, node))

	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(server.UUID)
	if err != nil {
		return nil, fmt.Errorf("invalid server UUID: %s", server.UUID)
	}

	providerID := cloudscaleProviderID{id: id}.String()
	addresses := serverNodeAddresses(server)

	klog.InfoS(
		"instance metadata found",
		"Node", node.Name,
		"Server", server.Name,
		"ProviderID", providerID,
	)

	return &cloudprovider.InstanceMetadata{
		ProviderID:    providerID,
		InstanceType:  server.Flavor.Slug,
		NodeAddresses: addresses,
		Zone:          server.Zone.Slug,
		Region:        strings.TrimRight(server.Zone.Slug, "0123456789"),
	}, nil
}
