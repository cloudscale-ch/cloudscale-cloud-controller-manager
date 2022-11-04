package cloudscale

import (
	"context"
	"github.com/cloudscale-ch/cloudscale-go-sdk"
	"golang.org/x/oauth2"
	"io"
	"os"
)
import cloudprovider "k8s.io/cloud-provider"

const (
	ProviderName = "cloudscale"
)

type cloud struct {
	client *cloudscale.Client
}

func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (i cloudprovider.Interface, err error) {
		return newCloud()
	})
}

func newCloud() (cloudprovider.Interface, error) {
	var token = os.Getenv("CLOUDSCALE_ACCESS_TOKEN")
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token,
	})
	oauthClient := oauth2.NewClient(context.Background(), tokenSource)
	var cloudscaleClient = cloudscale.NewClient(oauthClient)

	return &cloud{
		client: cloudscaleClient,
	}, nil
}

func (c cloud) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
	//TODO implement me
	panic("implement me")
}

func (c cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	//TODO implement me
	panic("implement me")
}

func (c cloud) Instances() (cloudprovider.Instances, bool) {
	//TODO implement me
	panic("implement me")
}

func (c cloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	//TODO implement me
	panic("implement me")
}

func (c cloud) Zones() (cloudprovider.Zones, bool) {
	//TODO implement me
	panic("implement me")
}

func (c cloud) Clusters() (cloudprovider.Clusters, bool) {
	//TODO implement me
	panic("implement me")
}

func (c cloud) Routes() (cloudprovider.Routes, bool) {
	//TODO implement me
	panic("implement me")
}

func (c cloud) ProviderName() string {
	//TODO implement me
	panic("implement me")
}

func (c cloud) HasClusterID() bool {
	//TODO implement me
	panic("implement me")
}
