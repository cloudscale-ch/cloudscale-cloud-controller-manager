package cloudscale

import (
	"context"
	"errors"
	"github.com/cloudscale-ch/cloudscale-go-sdk"
	"golang.org/x/oauth2"
	"io"
	"k8s.io/klog/v2"
	"os"
)
import cloudprovider "k8s.io/cloud-provider"

const (
	ProviderName = "cloudscale"
)

// cloud implements cloudprovider.Interface
type cloud struct {
	client *cloudscale.Client
}

// Register this provider with Kubernetes
func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, newCloudscaleProvider)
}

// newCloudscaleProvider creates the provider, ready to be registered
func newCloudscaleProvider(config io.Reader) (cloudprovider.Interface, error) {
	if config != nil {
		klog.Warning("--cloud-config received but ignored")
	}

	var token = os.Getenv("CLOUDSCALE_ACCESS_TOKEN")
	if len(token) == 0 {
		return nil, errors.New("no CLOUDSCALE_ACCESS_TOKEN configured")
	}

	return &cloud{
		client: newCloudscaleClient(token),
	}, nil
}

// newCloudscaleClient spawns a new cloudscale API client
func newCloudscaleClient(token string) *cloudscale.Client {
	klog.InfoS(
		"cloudscale API client",
		"url", os.Getenv("CLOUDSCALE_API_URL"),
		"token", os.Getenv("CLOUDSCALE_API_TOKEN")[:4]+"...",
	)
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token,
	})
	oauthClient := oauth2.NewClient(context.Background(), tokenSource)

	return cloudscale.NewClient(oauthClient)
}

// Initialize provides the cloud with a kubernetes client builder and may spawn goroutines
// to perform housekeeping or run custom controllers specific to the cloud provider.
// Any tasks started here should be cleaned up when the stop channel closes.
func (c cloud) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
	klog.Info("called Initialize")
}

// LoadBalancer returns a balancer interface. Also returns true if the interface is supported, false otherwise.
func (c cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	klog.Info("called LoadBalancer")
	return nil, false
}

// Instances returns an instances interface. Also returns true if the interface is supported, false otherwise.
func (c cloud) Instances() (cloudprovider.Instances, bool) {
	klog.Info("called Instances")
	return nil, false
}

// InstancesV2 is an implementation for instances and should only be implemented by external cloud providers.
// Implementing InstancesV2 is behaviorally identical to Instances but is optimized to significantly reduce
// API calls to the cloud provider when registering and syncing nodes. Implementation of this interface will
// disable calls to the Zones interface. Also returns true if the interface is supported, false otherwise.
func (c cloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	klog.Info("called InstancesV2")
	return nil, false
}

// Zones returns a zones interface. Also returns true if the interface is supported, false otherwise.
// DEPRECATED: Zones is deprecated in favor of retrieving zone/region information from InstancesV2.
// This interface will not be called if InstancesV2 is enabled.
func (c cloud) Zones() (cloudprovider.Zones, bool) {
	klog.Info("called Zones")
	return nil, false
}

// Clusters returns a clusters interface.  Also returns true if the interface is supported, false otherwise.
func (c cloud) Clusters() (cloudprovider.Clusters, bool) {
	klog.Info("called Clusters")
	return nil, false
}

// Routes returns a routes interface along with whether the interface is supported.
func (c cloud) Routes() (cloudprovider.Routes, bool) {
	klog.Info("called Routes")
	return nil, false
}

// ProviderName returns the cloud provider ID.
func (c cloud) ProviderName() string {
	klog.Info("called ProviderName")
	return ProviderName
}

// HasClusterID returns true if a ClusterID is required and set
func (c cloud) HasClusterID() bool {
	klog.Info("called HasClusterID")
	return false
}
