//go:build integration

package integration

import (
	"context"
	"time"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/cloudscale_ccm"
	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (s *IntegrationTestSuite) CreateDeployment(
	name string, image string, replicas int32, port int32, args []string) {

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
	name string, port int32, targetPort int32) {

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
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       spec,
		},
		metav1.CreateOptions{},
	)

	s.Require().NoError(err)
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

func (s *IntegrationTestSuite) TestServiceEndToEnd() {

	// Deploy a TCP server that returns the hostname
	s.CreateDeployment("hostname", "alpine/socat", 2, 8080, []string{
		`TCP-LISTEN:8080,fork`,
		`SYSTEM:'echo "${HOSTNAME}"`,
	})

	// Expose the deployment using a LoadBalancer service
	s.ExposeDeployment("hostname", 80, 8080)

	// Wait for the service to be ready
	var service *v1.Service
	start := time.Now()
	for time.Since(start) < 150*time.Second {
		service = s.ServiceNamed("hostname")
		s.Require().NotNil(service)

		if service.Annotations != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Ensure tha nnotations are set
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
	responses := make(map[string]int)
	for i := 0; i < 100; i++ {
		output, err := testkit.TCPRead(addr, 80)
		s.Assert().NoError(err)

		if output != "" {
			responses[output]++
		}

		time.Sleep(50 * time.Millisecond)
	}

	s.Assert().Len(responses, 2)
}
