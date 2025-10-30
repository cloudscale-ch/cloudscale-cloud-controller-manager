//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/cloudscale_ccm"
	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/kubeutil"
	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	cloudscale "github.com/cloudscale-ch/cloudscale-go-sdk/v6"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (s *IntegrationTestSuite) CreateDeployment(
	name string, image string, replicas int32, protocol v1.Protocol, port int32, args ...string) {

	var command []string

	if len(args) > 0 {
		command = args[:1]
		args = args[1:]
	}

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
						Name:    name,
						Image:   image,
						Command: command,
						Args:    args,
						Ports: []v1.ContainerPort{
							{ContainerPort: port, Protocol: protocol},
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

func (s *IntegrationTestSuite) CreateConfigMap(name string, data map[string]string) {
	_, err := s.k8s.CoreV1().ConfigMaps(s.ns).Create(
		context.Background(),
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Data: data,
		},
		metav1.CreateOptions{},
	)

	s.Require().NoError(err)
}

// ServicePortSpec defines the configuration for a single service port
type ServicePortSpec struct {
	Protocol   v1.Protocol
	Port       int32
	TargetPort int32
}

func (s *IntegrationTestSuite) ExposeDeployment(
	name string, annotations map[string]string, ports ...ServicePortSpec) {

	servicePorts := make([]v1.ServicePort, len(ports))
	for i, p := range ports {
		servicePorts[i] = v1.ServicePort{
			Name:       fmt.Sprintf("port%d", i),
			Protocol:   p.Protocol,
			Port:       p.Port,
			TargetPort: intstr.FromInt32(p.TargetPort),
		}
	}

	spec := v1.ServiceSpec{
		Type: v1.ServiceTypeLoadBalancer,
		Selector: map[string]string{
			"app": name,
		},
		Ports: servicePorts,
	}

	service, err := s.k8s.CoreV1().Services(s.ns).Get(
		context.Background(), name, metav1.GetOptions{},
	)

	if err != nil {
		_, err = s.k8s.CoreV1().Services(s.ns).Create(
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
	} else {
		service.Spec = spec
		service.ObjectMeta.Annotations = annotations

		_, err = s.k8s.CoreV1().Services(s.ns).Update(
			context.Background(),
			service,
			metav1.UpdateOptions{},
		)
		s.Require().NoError(err)
	}
}

// RunJob starts a single job and then awaits the result, returing it as string
func (s *IntegrationTestSuite) RunJob(
	image string, timeout time.Duration, cmd ...string) string {

	ctx, _ := context.WithTimeout(context.Background(), timeout)
	name := fmt.Sprintf("job-%08x", rand.Uint32())

	// Specify the job
	spec := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: batchv1.JobSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					RestartPolicy: v1.RestartPolicyNever,
					Containers: []v1.Container{
						{
							Name:    name,
							Image:   image,
							Command: cmd,
						},
					},
				},
			},
		},
	}

	// Start it
	_, err := s.k8s.BatchV1().Jobs(s.ns).Create(
		ctx,
		&spec,
		metav1.CreateOptions{},
	)

	s.Require().NoError(err)

	// Wait for completion
	var job *batchv1.Job
	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			job, err = s.k8s.BatchV1().Jobs(s.ns).Get(
				ctx, name, metav1.GetOptions{})

			if err != nil {
				return false, err
			}
			return job.Status.Succeeded > 0, nil
		},
	)

	s.Require().NoError(err)

	// Get pod
	pods, err := s.k8s.CoreV1().Pods(s.ns).List(
		ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("job-name=%s", name),
		},
	)

	s.Require().NoError(err)
	s.Require().Len(pods.Items, 1)

	logs, err := s.k8s.CoreV1().Pods(s.ns).GetLogs(
		pods.Items[0].Name, &v1.PodLogOptions{}).Do(ctx).Raw()

	s.Require().NoError(err)

	return string(logs)
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
			uuid := service.Annotations["k8s.cloudscale.ch/loadbalancer-uuid"]

			// EnsureLoadBalancer sets the annotation, and then returns the
			// load balancer status to Kubernetes. This means there is a short
			// window between setting the annotation, and the service receving
			// its load balancer configuration.
			//
			// To avoid races, we therefore have to check for the annotation,
			// as well as the load balancer state.
			if uuid != "" && len(service.Status.LoadBalancer.Ingress) > 0 {
				return service
			}
		}

		time.Sleep(1 * time.Second)
	}

	return nil
}

func (s *IntegrationTestSuite) TestServiceEndToEnd() {

	// Note the start for the log
	start := time.Now()

	// Deploy a TCP server that returns the hostname
	s.T().Log("Creating nginx deployment")
	s.CreateDeployment("nginx", "nginxdemos/hello:plain-text", 2, v1.ProtocolTCP, 80)

	// Expose the deployment using a LoadBalancer service
	s.ExposeDeployment("nginx", nil,
		ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 80, TargetPort: 80})

	// Wait for the service to be ready
	s.T().Log("Waiting for nginx service to be ready")
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

func (s *IntegrationTestSuite) TestServiceEndToEndUDP() {

	// Note the start for the log
	start := time.Now()

	// Deploy a UDP echo server
	s.T().Log("Creating udp-echo deployment")
	s.CreateDeployment("udp-echo", "docker.io/alpine/socat", 2, v1.ProtocolUDP, 5353,
		"socat",
		"-v",
		"UDP4-RECVFROM:5353,fork",
		"SYSTEM:echo 'I could tell you a UDP joke, but you might not get it...',pipes",
	)

	// Expose the deployment using a LoadBalancer service with UDP annotations
	s.ExposeDeployment("udp-echo", map[string]string{
		"k8s.cloudscale.ch/loadbalancer-health-monitor-type":      "udp-connect",
		"k8s.cloudscale.ch/loadbalancer-health-monitor-delay-s":   "3",
		"k8s.cloudscale.ch/loadbalancer-health-monitor-timeout-s": "2",
	}, ServicePortSpec{Protocol: v1.ProtocolUDP, Port: 5000, TargetPort: 5353})

	// Wait for the service to be ready
	s.T().Log("Waiting for udp-echo service to be ready")
	service := s.AwaitServiceReady("udp-echo", 180*time.Second)
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

	// Verify UDP service responses using Go's UDP client
	s.T().Log("Verifying UDP echo service responses")
	errors := 0
	successes := 0

	// Create UDP client
	conn, err := net.Dial("udp", fmt.Sprintf("%s:5000", addr))
	s.Require().NoError(err)
	s.T().Log("UDP client connected successfully")
	defer conn.Close()

	// Set read timeout
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	message := []byte("Tell me a joke")
	for i := 0; i < 10; i++ {
		// Send message
		_, err := conn.Write(message)
		if err != nil {
			s.T().Logf("Failed to send message %d: %s", i, err)
			errors++
			continue
		}

		// Read response
		buffer := make([]byte, 1024)
		n, err := conn.Read(buffer)
		if err != nil {
			s.T().Logf("Failed to read response %d: %s", i, err)
			errors++
			continue
		}

		response := string(buffer[:n])
		if strings.Contains(response, "UDP joke") {
			successes++
		}

		time.Sleep(250 * time.Millisecond)
	}

	// Expect most requests to succeed
	s.T().Logf("Successful probes: %d, Error probes: %d", successes, errors)
	s.Assert().GreaterOrEqual(successes, 8)

	// In this simple case we expect no errors nor warnings
	s.T().Log("Checking log output for errors/warnings")
	lines := s.CCMLogs(start)

	s.Assert().NotContains(lines, "error")
	s.Assert().NotContains(lines, "Error")
	s.Assert().NotContains(lines, "warn")
	s.Assert().NotContains(lines, "Warn")
}

func (s *IntegrationTestSuite) TestServiceEndToEndDualProtocol() {

	// Note the start for the log
	start := time.Now()

	// Deploy a DNS server that handles both TCP and UDP
	s.T().Log("Creating dns-server deployment")

	// Create the ConfigMap for CoreDNS configuration
	s.CreateConfigMap("coredns-config", map[string]string{
		"Corefile": `.:53 {
    log
    errors
    health
    ready
    whoami
    forward . 8.8.8.8 9.9.9.9
}`,
	})

	// Create deployment with both TCP and UDP ports
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "dns-server"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "dns-server",
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "dns-server",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "coredns",
							Image: "coredns/coredns:1.11.1",
							Args:  []string{"-conf", "/etc/coredns/Corefile"},
							Ports: []v1.ContainerPort{
								{ContainerPort: 53, Protocol: v1.ProtocolUDP, Name: "dns-udp"},
								{ContainerPort: 53, Protocol: v1.ProtocolTCP, Name: "dns-tcp"},
							},
							VolumeMounts: []v1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/etc/coredns",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "config",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "coredns-config",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := s.k8s.AppsV1().Deployments(s.ns).Create(
		context.Background(),
		deployment,
		metav1.CreateOptions{},
	)
	s.Require().NoError(err)

	s.ExposeDeployment("dns-server", nil,
		ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 53, TargetPort: 53},
		ServicePortSpec{Protocol: v1.ProtocolUDP, Port: 53, TargetPort: 53},
	)

	// Wait for the service to be ready
	s.T().Log("Waiting for dns-server service to be ready")
	svc := s.AwaitServiceReady("dns-server", 180*time.Second)
	s.Require().NotNil(svc)

	// Ensure the annotations are set
	s.Assert().NotEmpty(
		svc.Annotations[cloudscale_ccm.LoadBalancerUUID])
	s.Assert().NotEmpty(
		svc.Annotations[cloudscale_ccm.LoadBalancerConfigVersion])
	s.Assert().NotEmpty(
		svc.Annotations[cloudscale_ccm.LoadBalancerZone])

	// Ensure we have two public IP addresses
	s.Require().Len(svc.Status.LoadBalancer.Ingress, 2)
	addr := svc.Status.LoadBalancer.Ingress[0].IP

	// Create custom resolver pointing to the DNS service
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, network, fmt.Sprintf("%s:53", addr))
		},
	}

	// Test UDP DNS queries (default)
	s.T().Log("Verifying UDP DNS service responses")
	udpSuccesses := 0
	udpErrors := 0

	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		mx, err := resolver.LookupMX(ctx, "cloudscale.ch")
		cancel()
		s.Require().NoError(err)
		s.Require().Len(mx, 1)
		s.Assert().Equal("mail.cloudscale.ch.", mx[0].Host)

		if err != nil {
			s.T().Logf("UDP query %d failed: %s", i, err)
			udpErrors++
		} else {
			udpSuccesses++
		}

		time.Sleep(250 * time.Millisecond)
	}

	s.T().Logf("UDP - Successful queries: %d, Errors: %d", udpSuccesses, udpErrors)
	s.Assert().GreaterOrEqual(udpSuccesses, 8)

	// Test TCP DNS queries
	s.T().Log("Verifying TCP DNS service responses")
	tcpResolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			// Force TCP by specifying "tcp" explicitly
			return d.DialContext(ctx, "tcp", fmt.Sprintf("%s:53", addr))
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	mx, err := tcpResolver.LookupMX(ctx, "cloudscale.ch")
	cancel()
	s.Require().NoError(err)
	s.Require().Len(mx, 1)
	s.Assert().Equal("mail.cloudscale.ch.", mx[0].Host)

	tcpSuccesses := 0

	if err != nil {
		s.T().Logf("TCP query failed: %s", err)
	} else {
		tcpSuccesses++
	}

	s.T().Logf("TCP - Successful queries: %d", tcpSuccesses)
	s.Assert().Equal(tcpSuccesses, 1)

	// In this simple case we expect no errors nor warnings
	s.T().Log("Checking log output for errors/warnings")
	lines := s.CCMLogs(start)

	s.Assert().NotContains(lines, "error")
	s.Assert().NotContains(lines, "Error")
	s.Assert().NotContains(lines, "warn")
	s.Assert().NotContains(lines, "Warn")
}

func (s *IntegrationTestSuite) TestServiceVIPAddresses() {

	// Get the private subnet used by the nodes
	var subnet string
	var public string

	servers := s.Servers()
	s.Require().NotEmpty(servers)
	for _, iface := range servers[0].Interfaces {
		if iface.Type == "public" {
			public = iface.Addresses[0].Address
			continue
		}

		subnet = iface.Addresses[0].Subnet.UUID
		break
	}

	// Deploy a TCP server that returns something
	s.T().Log("Creating foo deployment")
	s.CreateDeployment("nginx", "nginxdemos/hello:plain-text", 2, v1.ProtocolTCP, 80)

	// Expose the deployment using a LoadBalancer service
	s.ExposeDeployment("nginx", map[string]string{
		"k8s.cloudscale.ch/loadbalancer-vip-addresses": fmt.Sprintf(
			`[{"subnet": "%s"}]`, subnet),
	}, ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 80, TargetPort: 80})

	s.T().Log("Waiting for nginx service to be ready")
	service := s.AwaitServiceReady("nginx", 180*time.Second)
	s.Require().NotNil(service)

	// Use a worker as a jumphost to check if we get "foo"
	addr := service.Status.LoadBalancer.Ingress[0].IP

	cmd := exec.Command(
		"ssh", fmt.Sprintf("ubuntu@%s", public), "-i", s.sshkey,
		fmt.Sprintf(
			"curl -s --retry 3 --retry-delay 3 --retry-all-errors http://%s",
			addr,
		),
	)
	out, err := cmd.Output()
	s.Require().NoError(err)
	s.Require().Contains(string(out), "Server name:")
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
	s.CreateDeployment("peeraddr", "ghcr.io/majd/ip-curl", 1, v1.ProtocolTCP, 3000)

	// Waits until the request is received through the given prefix and
	// ten responses with the expected address come back.
	assertPrefix := func(addr string, prefix *netip.Prefix) {
		url := fmt.Sprintf("http://%s", addr)
		successes := 0
		successesRequired := 15
		start := time.Now()
		timeout := 300

		for i := 0; i < timeout; i++ {
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

			successes++

			if successes >= successesRequired {
				break
			}
		}

		took := time.Since(start).Round(time.Second)

		if successes >= successesRequired {
			s.T().Logf("Took %s %s to get ready", url, took)
		} else {
			s.T().Logf("Took %s too long to get ready (%s)", url, took)
			s.T().Logf("CCM Logs: \n%s", s.CCMLogs(start))
		}

		s.Require().GreaterOrEqual(successes, successesRequired)
	}

	// Ensures the traffic is handled without unexpected delay
	assertFastResponses := func(addr string, _ *netip.Prefix) {
		url := fmt.Sprintf("http://%s", addr)
		for i := 0; i < 60; i++ {
			before := time.Now()
			_, err := testkit.HTTPRead(url)
			after := time.Now()

			// Bad requests take around 5s as they hit a timeout
			s.Assert().WithinDuration(before, after, 1500*time.Millisecond)
			s.Assert().NoError(err)
		}
	}

	// Expose the deployment using a LoadBalancer service
	s.ExposeDeployment("peeraddr", nil, ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 80, TargetPort: 3000})

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

func (s *IntegrationTestSuite) TestServiceWithGlobalFloatingIP() {
	global, err := s.CreateGlobalFloatingIP()
	s.Require().NoError(err)
	s.RunTestServiceWithFloatingIP(global)
}

func (s *IntegrationTestSuite) TestServiceWithRegionalFloatingIP() {
	regional, err := s.CreateRegionalFloatingIP(s.Region())
	s.Require().NoError(err)
	s.RunTestServiceWithFloatingIP(regional)
}

func (s *IntegrationTestSuite) RunTestServiceWithFloatingIP(
	fip *cloudscale.FloatingIP) {

	// Deploy a TCP server that returns the hostname
	s.T().Log("Creating nginx deployment")
	s.CreateDeployment("nginx", "nginxdemos/hello:plain-text", 2, v1.ProtocolTCP, 80)

	// Expose the deployment using a LoadBalancer service with Floating IP
	s.ExposeDeployment("nginx", map[string]string{
		"k8s.cloudscale.ch/loadbalancer-floating-ips": fmt.Sprintf(
			`["%s"]`, fip.Network),
	}, ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 80, TargetPort: 80})

	// Wait for the service to be ready
	s.T().Log("Waiting for nginx service to be ready")
	service := s.AwaitServiceReady("nginx", 180*time.Second)
	s.Require().NotNil(service)

	// Ensure that we get responses from two different pods (round-robin)
	s.T().Log("Verifying hostname service responses")
	addr := strings.SplitN(fip.Network, "/", 2)[0]
	responses := make(map[string]int)
	errors := 0
	bound := false

	for i := 0; i < 100; i++ {
		response, err := testkit.HelloNginx(addr, 80)

		// The first 25 requests may err, as the Floating IP has to propagate
		if err != nil && !bound {
			continue
		}

		if err == nil && !bound {
			s.Assert().LessOrEqual(errors, 25)
			bound = true
			errors = 0
		}

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

	// Allow for errors, which occurs maybe once in the first 100 requests
	// to a service, and which does not occur anymore later (even when
	// running for a long time).
	s.Assert().LessOrEqual(errors, 1)
	s.Assert().Len(responses, 2)
}

func (s *IntegrationTestSuite) TestFloatingIPConflicts() {

	// Create a regional floating IP
	regional, err := s.CreateRegionalFloatingIP(s.Region())
	s.Require().NoError(err)

	// Deploy a TCP server that returns the hostname
	s.T().Log("Creating nginx deployment")
	s.CreateDeployment("nginx", "nginxdemos/hello:plain-text", 2, v1.ProtocolTCP, 80)

	// Expose the deployment using a LoadBalancer service with Floating IP
	s.ExposeDeployment("nginx", map[string]string{
		"k8s.cloudscale.ch/loadbalancer-floating-ips": fmt.Sprintf(
			`["%s"]`, regional.Network),
	}, ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 80, TargetPort: 80})

	// Wait for the service to be ready
	s.T().Log("Waiting for nginx service to be ready")
	service := s.AwaitServiceReady("nginx", 180*time.Second)
	s.Require().NotNil(service)

	// Configure a second service with the same floating IP
	start := time.Now()

	s.ExposeDeployment("service-2", map[string]string{
		"k8s.cloudscale.ch/loadbalancer-floating-ips": fmt.Sprintf(
			`["%s"]`, regional.Network),
	}, ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 80, TargetPort: 80})

	// Wait for a moment before checking the log
	time.Sleep(5 * time.Second)

	// Ensure the conflict was detected
	lines := s.CCMLogs(start)
	s.Assert().Contains(lines, "assigned to another service")
}

func (s *IntegrationTestSuite) TestServiceProxyProtocol() {

	// Get the branch to run http-echo with (in the future, we might
	// offer this in a separate container).
	branch := os.Getenv("HTTP_ECHO_BRANCH")
	if len(branch) == 0 {
		branch = "main"
	}

	// Deploy our http-echo server to check for proxy connections
	s.T().Log("Creating http-echo deployment", "branch", branch)
	s.CreateDeployment("http-echo", "golang", 2, v1.ProtocolTCP, 80, "bash", "-c", fmt.Sprintf(`
  		git clone https://github.com/cloudscale-ch/cloudscale-cloud-controller-manager ccm;
	  	cd ccm;
  		git checkout %s || exit 1;
  		cd cmd/http-echo;
  		go run main.go -host 0.0.0.0 -port 80
	`, branch))

	// Expose the deployment using a LoadBalancer service
	s.ExposeDeployment("http-echo", map[string]string{
		"k8s.cloudscale.ch/loadbalancer-pool-protocol": "proxy",

		// Make sure to get the default behavior of older Kubernetes releases,
		// even on newer releases.
		"k8s.cloudscale.ch/loadbalancer-ip-mode": "VIP",
	}, ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 80, TargetPort: 80})

	// Wait for the service to be ready
	s.T().Log("Waiting for http-echo service to be ready")
	service := s.AwaitServiceReady("http-echo", 180*time.Second)
	s.Require().NotNil(service)

	addr := service.Status.LoadBalancer.Ingress[0].IP
	url := fmt.Sprintf("http://%s/proxy-protocol/used", addr)

	// Wait for respones to work
	s.T().Log("Waiting for http-echo responses")
	errors := 0

	for i := 0; i < 100; i++ {
		_, err := testkit.HTTPRead(url)

		if err == nil {
			break
		} else {
			s.T().Logf("Request %d failed: %s", i, err)
			errors++
		}

		time.Sleep(5 * time.Millisecond)
	}

	// Make sure our HTTP requests get wrapped in the PROXY protocol
	s.T().Log("Testing PROXY protocol from outside")

	used, err := testkit.HTTPRead(url)
	s.Assert().NoError(err)
	s.Assert().Equal("true\n", used)

	// Sending a request from inside the cluster does not work, unless we
	// use a workaround.
	s.T().Log("Testing PROXY protocol from inside")
	used = s.RunJob("curlimages/curl", 90*time.Second, "curl", "-s", url)
	s.Assert().Equal("false\n", used)

	// The workaround works by using an IP that needs to be reolved via name
	s.ExposeDeployment("http-echo", map[string]string{
		"k8s.cloudscale.ch/loadbalancer-pool-protocol": "proxy",
		"k8s.cloudscale.ch/loadbalancer-ip-mode":       "VIP",
		"k8s.cloudscale.ch/loadbalancer-force-hostname": fmt.Sprintf(
			"%s.cust.cloudscale.ch",
			strings.ReplaceAll(addr, ".", "-"),
		),
	}, ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 80, TargetPort: 80})

	s.T().Log("Testing PROXY protocol from inside with workaround")
	used = s.RunJob("curlimages/curl", 90*time.Second, "curl", "-s", url)
	s.Assert().Equal("true\n", used)

	// On newer Kubernetes releases, the defaults just work
	newer, err := kubeutil.IsKubernetesReleaseOrNewer(s.k8s, 1, 30)
	s.Assert().NoError(err)

	if newer {
		s.ExposeDeployment("http-echo", map[string]string{
			"k8s.cloudscale.ch/loadbalancer-pool-protocol": "proxy",
		}, ServicePortSpec{Protocol: v1.ProtocolTCP, Port: 80, TargetPort: 80})

		s.T().Log("Testing PROXY protocol on newer Kubernetes releases")
		used = s.RunJob("curlimages/curl", 90*time.Second, "curl", "-s", url)
		s.Assert().Equal("true\n", used)
	}
}
