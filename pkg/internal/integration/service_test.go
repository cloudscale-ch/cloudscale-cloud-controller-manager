//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"time"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/cloudscale_ccm"
	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/kubeutil"
	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (s *IntegrationTestSuite) CreateDeployment(
	name string, image string, replicas int32, port int32, args ...string) {

	spec := appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": name,
			},
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app": name,
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  name,
						Image: image,
						Args:  args,
						Ports: []v1.ContainerPort{
							{ContainerPort: port},
						},
					},
				},
			},
		},
	}

	_, err := s.k8s.AppsV1().Deployments(s.ns).Create(
		context.Background(),
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       spec,
		},
		metav1.CreateOptions{},
	)

	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) ExposeDeployment(
	name string, port int32, targetPort int32, annotations map[string]string) {

	spec := v1.ServiceSpec{
		Type: v1.ServiceTypeLoadBalancer,
		Selector: map[string]string{
			"app": name,
		},
		Ports: []v1.ServicePort{
			{
				Protocol:   v1.ProtocolTCP,
				Port:       port,
				TargetPort: intstr.FromInt32(targetPort),
			},
		},
	}

	_, err := s.k8s.CoreV1().Services(s.ns).Create(
		context.Background(),
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: annotations,
			},
			Spec: spec,
		},
		metav1.CreateOptions{},
	)

	s.Require().NoError(err)
}

// CCMLogs returns all the logs of the CCM since the given time.
func (s *IntegrationTestSuite) CCMLogs(start time.Time) string {

	pods, err := s.k8s.CoreV1().Pods("kube-system").List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: "k8s-app=cloudscale-cloud-controller-manager",
		},
	)
	s.Require().NoError(err)

	st := metav1.NewTime(start)
	options := v1.PodLogOptions{
		SinceTime: &st,
	}

	output := ""
	for _, pod := range pods.Items {
		logs := s.k8s.CoreV1().
			Pods("kube-system").
			GetLogs(pod.Name, &options)

		stream, err := logs.Stream(context.Background())
		s.Require().NoError(err)
		defer stream.Close()

		bytes, err := io.ReadAll(stream)
		s.Require().NoError(err)

		output += string(bytes)
	}

	return output
}

func (s *IntegrationTestSuite) ServiceNamed(name string) *v1.Service {
	service, err := s.k8s.CoreV1().Services(s.ns).Get(
		context.Background(), name, metav1.GetOptions{},
	)

	if err != nil && errors.IsNotFound(err) {
		return nil
	}

	s.Require().NoError(err)
	return service
}

func (s *IntegrationTestSuite) AwaitServiceReady(
	name string, timeout time.Duration) *v1.Service {

	var service *v1.Service
	start := time.Now()

	for time.Since(start) < timeout {
		service = s.ServiceNamed(name)
		s.Require().NotNil(service)

		if service.Annotations != nil {
			return service
		}
		time.Sleep(1 * time.Second)
	}

	return nil
}

func (s *IntegrationTestSuite) TestServiceEndToEnd() {

	// Note the start for the log
	start := time.Now()

	// Deploy a TCP server that returns the hostname
	s.T().Log("Creating hostname deployment")
	s.CreateDeployment("nginx", "nginxdemos/hello:plain-text", 2, 80)

	// Expose the deployment using a LoadBalancer service
	s.ExposeDeployment("nginx", 80, 80, nil)

	// Wait for the service to be ready
	s.T().Log("Waiting for hostname service to be ready")
	service := s.AwaitServiceReady("nginx", 180*time.Second)
	s.Require().NotNil(service)

	// Ensure the annotations are set
	s.Assert().NotEmpty(
		service.Annotations[cloudscale_ccm.LoadBalancerUUID])
	s.Assert().NotEmpty(
		service.Annotations[cloudscale_ccm.LoadBalancerConfigVersion])
	s.Assert().NotEmpty(
		service.Annotations[cloudscale_ccm.LoadBalancerZone])

	// Ensure we have two public IP addresses
	s.Require().Len(service.Status.LoadBalancer.Ingress, 2)
	addr := service.Status.LoadBalancer.Ingress[0].IP

	// Ensure that we get responses from two different pods (round-robin)
	s.T().Log("Verifying hostname service responses")
	responses := make(map[string]int)
	errors := 0
	for i := 0; i < 100; i++ {
		response, err := testkit.HelloNginx(addr, 80)
		if err != nil {
			s.T().Logf("Request %d failed: %s", i, err)
			errors++
		}

		if response != nil {
			s.Assert().NotEmpty(response.ServerName)
			responses[response.ServerName]++
		}

		time.Sleep(5 * time.Millisecond)
	}

	// Allow for one error, which occurs maybe once in the first 100 requests
	// to a service, and which does not occur anymore later (even when
	// running for a long time).
	s.Assert().LessOrEqual(errors, 1)
	s.Assert().Len(responses, 2)

	// In this simple case we expect no errors nor warnings
	s.T().Log("Checking log output for errors/warnings")
	lines := s.CCMLogs(start)

	s.Assert().NotContains(lines, "error")
	s.Assert().NotContains(lines, "Error")
	s.Assert().NotContains(lines, "warn")
	s.Assert().NotContains(lines, "Warn")
}

func (s *IntegrationTestSuite) TestServiceTrafficPolicyLocal() {

	// Traffic received via default "Cluster" policy is snatted via node.
	cluster_policy_prefix := netip.MustParsePrefix("10.0.0.0/16")

	// Traffic received via "Local" policy has no natting. The address is
	// going to be private network address of the load balancer.
	local_policy_prefix := netip.MustParsePrefix("10.100.0.0/16")

	// Deploy a TCP server that returns the remote IP address. Only use a
	// single instance as we want to check that the routing works right with
	// all policies.
	s.T().Log("Creating peeraddr deployment")
	s.CreateDeployment("peeraddr", "ghcr.io/majd/ip-curl", 1, 3000)

	// Waits until the request is received through the given prefix and
	// ten responses with the expected address come back.
	assertPrefix := func(addr string, prefix *netip.Prefix) {
		url := fmt.Sprintf("http://%s", addr)
		successful := 0

		for i := 0; i < 45; i++ {
			time.Sleep(1 * time.Second)

			peer, err := testkit.HTTPRead(url)
			if err != nil {
				continue
			}

			if strings.Trim(peer, "\n") == "" {
				continue
			}

			peerIP := netip.MustParseAddr(strings.Trim(peer, "\n"))
			if !prefix.Contains(peerIP) {
				continue
			}

			successful++

			if successful >= 15 {
				break
			}
		}

		s.Assert().GreaterOrEqual(successful, 15)
	}

	// Ensures the traffic is handled without unexpected delay
	assertFastResponses := func(addr string, prefix *netip.Prefix) {
		url := fmt.Sprintf("http://%s", addr)
		for i := 0; i < 60; i++ {
			before := time.Now()
			_, err := testkit.HTTPRead(url)
			after := time.Now()

			// Bad requests take around 5s as they hit a timeout
			s.Assert().WithinDuration(before, after, 1000*time.Millisecond)
			s.Assert().NoError(err)
		}
	}

	// Expose the deployment using a LoadBalancer service
	s.ExposeDeployment("peeraddr", 80, 3000, nil)

	// Wait for the service to be ready
	s.T().Log("Waiting for peeraddr service to be ready")
	service := s.AwaitServiceReady("peeraddr", 180*time.Second)
	s.Require().NotNil(service)

	// In its initial state, expect a natted IP address
	addr := service.Status.LoadBalancer.Ingress[0].IP

	assertPrefix(addr, &cluster_policy_prefix)
	assertFastResponses(addr, &cluster_policy_prefix)

	// Configure the service to use the local traffic policy
	s.T().Log("Switching peeraddr service to 'Local' traffic policy")
	err := kubeutil.PatchServiceExternalTrafficPolicy(
		context.Background(),
		s.k8s,
		service,
		v1.ServiceExternalTrafficPolicyTypeLocal,
	)
	s.Require().NoError(err)

	service = s.AwaitServiceReady("peeraddr", 1*time.Second)
	s.Require().NotNil(service)

	// Now expect to see an IP address from the node's private network
	assertPrefix(addr, &local_policy_prefix)
	assertFastResponses(addr, &local_policy_prefix)

	// Go back to the Cluster policy
	s.T().Log("Switching peeraddr service back to 'Cluster' traffic policy")
	err = kubeutil.PatchServiceExternalTrafficPolicy(
		context.Background(),
		s.k8s,
		service,
		v1.ServiceExternalTrafficPolicyTypeCluster,
	)
	s.Require().NoError(err)

	service = s.AwaitServiceReady("peeraddr", 1*time.Second)
	s.Require().NotNil(service)

	assertPrefix(addr, &cluster_policy_prefix)
	assertFastResponses(addr, &cluster_policy_prefix)
}
