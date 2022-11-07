package cloudscale

import (
	"context"
	"github.com/cloudscale-ch/cloudscale-go-sdk"
	"k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
)

type loadbalancers struct {
	client *cloudscale.Client
}

func newLoadbalancers(cloudscaleClient *cloudscale.Client) cloudprovider.LoadBalancer {
	return &loadbalancers{
		client: cloudscaleClient,
	}
}

func (l loadbalancers) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (status *v1.LoadBalancerStatus, exists bool, err error) {
	//TODO implement me
	panic("implement me")
}

func (l loadbalancers) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	//TODO implement me
	panic("implement me")
}

func (l loadbalancers) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	//TODO implement me
	panic("implement me")
}

func (l loadbalancers) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	//TODO implement me
	panic("implement me")
}

func (l loadbalancers) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	//TODO implement me
	panic("implement me")
}
