package cloudscale_ccm

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	cloudscale "github.com/cloudscale-ch/cloudscale-go-sdk/v3"
	"golang.org/x/oauth2"
	"k8s.io/klog/v2"

	cloudprovider "k8s.io/cloud-provider"
)

const (
	// Under no circumstances can this string change. It is for eterentiy.
	ProviderName   = "cloudscale"
	AccessToken    = "CLOUDSCALE_ACCESS_TOKEN"
	ApiUrl         = "CLOUDSCALE_API_URL"
	ApiTimeout     = "CLOUDSCALE_API_TIMEOUT"
	DefaultTimeout = time.Duration(15) * time.Second
)

// cloud implements cloudprovider.Interface
type cloud struct {
	instances    *instances
	loadbalancer *loadbalancer
}

// Register this provider with Kubernetes
func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, newCloudscaleProvider)
}

// maskAccessToken returns the given token with most of the information hidden
func maskAccessToken(token string) string {
	if len(token) < 4 {
		return ""
	}
	return fmt.Sprintf("%.4s%s", token, strings.Repeat("*", len(token)-4))
}

// apiTimeout returns the configured timeout or the default one
func apiTimeout() time.Duration {
	if seconds, _ := strconv.Atoi(os.Getenv(ApiTimeout)); seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return DefaultTimeout
}

// newCloudscaleProvider creates the provider, ready to be registered
func newCloudscaleProvider(config io.Reader) (cloudprovider.Interface, error) {
	if config != nil {
		klog.Warning("--cloud-config received but ignored")
	}

	var token = os.Getenv(AccessToken)
	if len(token) == 0 {
		return nil, fmt.Errorf("no %s configured", AccessToken)
	}

	client := newCloudscaleClient(token, apiTimeout())

	return &cloud{
		instances: &instances{
			srv: serverMapper{client: client},
		},
	}, nil
}

// newCloudscaleClient spawns a new cloudscale API client
func newCloudscaleClient(token string, timeout time.Duration) *cloudscale.Client {

	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token,
	})

	httpClient := oauth2.NewClient(context.Background(), tokenSource)
	httpClient.Timeout = timeout

	klog.InfoS(
		"cloudscale API client",
		"url", os.Getenv(ApiUrl),
		"token", maskAccessToken(os.Getenv(AccessToken)),
		"timeout", timeout,
	)

	return cloudscale.NewClient(httpClient)
}

// Initialize provides the cloud with a kubernetes client builder and may spawn goroutines
// to perform housekeeping or run custom controllers specific to the cloud provider.
// Any tasks started here should be cleaned up when the stop channel closes.
func (c cloud) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
}

// LoadBalancer returns a balancer interface. Also returns true if the interface is supported, false otherwise.
func (c cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return nil, false
}

// Instances returns an instances interface. Also returns true if the interface is supported, false otherwise.
func (c cloud) Instances() (cloudprovider.Instances, bool) {
	return nil, false
}

// InstancesV2 is an implementation for instances and should only be implemented by external cloud providers.
// Implementing InstancesV2 is behaviorally identical to Instances but is optimized to significantly reduce
// API calls to the cloud provider when registering and syncing nodes. Implementation of this interface will
// disable calls to the Zones interface. Also returns true if the interface is supported, false otherwise.
func (c cloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return c.instances, true
}

// Zones returns a zones interface. Also returns true if the interface is supported, false otherwise.
// DEPRECATED: Zones is deprecated in favor of retrieving zone/region information from InstancesV2.
// This interface will not be called if InstancesV2 is enabled.
func (c cloud) Zones() (cloudprovider.Zones, bool) {
	return nil, false
}

// Clusters returns a clusters interface.  Also returns true if the interface is supported, false otherwise.
func (c cloud) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

// Routes returns a routes interface along with whether the interface is supported.
func (c cloud) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

// ProviderName returns the cloud provider ID.
func (c cloud) ProviderName() string {
	return ProviderName
}

// HasClusterID returns true if a ClusterID is required and set
func (c cloud) HasClusterID() bool {
	return false
}
