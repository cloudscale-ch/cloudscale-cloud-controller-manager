// Implements the cloudscale-cloud-controller-manager CLI
package main

import (
	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/cloudscale"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/app"
	"k8s.io/cloud-provider/app/config"
	"k8s.io/cloud-provider/options"
	"k8s.io/component-base/cli"
	cliflag "k8s.io/component-base/cli/flag"
	_ "k8s.io/component-base/metrics/prometheus/clientgo" // load all the prometheus client-go plugins
	_ "k8s.io/component-base/metrics/prometheus/version"  // for version metric registration
	"k8s.io/klog/v2"
	"os"
)

func main() {
	ccmOptions, err := options.NewCloudControllerManagerOptions()

	// Always set --cloud-provider, as running this binary with another
	// provider is nonsensical.
	ccmOptions.KubeCloudShared.CloudProvider.Name = cloudscale.ProviderName

	if err != nil {
		klog.Fatalf("unable to initialize command options: %v", err)
	}

	cmd := app.NewCloudControllerManagerCommand(
		ccmOptions,
		cloudInitializer,
		app.DefaultInitFuncConstructors,
		cliflag.NamedFlagSets{},
		wait.NeverStop)

	os.Exit(cli.Run(cmd))
}

func cloudInitializer(config *config.CompletedConfig) cloudprovider.Interface {
	cloudConfig := config.ComponentConfig.KubeCloudShared.CloudProvider

	cloud, err := cloudprovider.InitCloudProvider(
		cloudConfig.Name, cloudConfig.CloudConfigFile)

	if err != nil {
		klog.Fatalf("Cloud provider could not be initialized: %v", err)
	}
	if cloud == nil {
		klog.Fatalf("Cloud provider is nil")
	}

	if !cloud.HasClusterID() {
		if config.ComponentConfig.KubeCloudShared.AllowUntaggedCloud {
			klog.Warning("Detected a cluster without a ClusterID. " +
				"A ClusterID will be required in the future. " +
				"Please tag your cluster to avoid any future issues")
		} else {
			klog.Fatalf("No ClusterID found. A ClusterID is required " +
				"for the cloud provider to function properly. " +
				"This check can be bypassed by setting the " +
				"allow-untagged-cloud option")
		}
	}

	klog.Info("cloudscale-cloud-controller-manager initialized")

	return cloud
}
