//go:build integration

package integration

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"testing"
	"time"

	cloudscale "github.com/cloudscale-ch/cloudscale-go-sdk/v6"
	"github.com/stretchr/testify/suite"
	"golang.org/x/oauth2"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestMain(m *testing.M) {
	exitStatus := m.Run()
	os.Exit(exitStatus)
}

func TestIntegration(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

type IntegrationTestSuite struct {
	suite.Suite
	k8s           kubernetes.Interface
	api           *cloudscale.Client
	ns            string
	clusterPrefix string
	resources     []string
	sshkey        string
}

func (s *IntegrationTestSuite) SetupSuite() {
	// Kubernetes client
	k8test, ok := os.LookupEnv("K8TEST_PATH")
	if !ok {
		log.Fatalf("could not find K8TEST_PATH environment variable\n")
	}
	s.sshkey = fmt.Sprintf("%s/cluster/ssh", k8test)

	if prefix, ok := os.LookupEnv("CLUSTER_PREFIX"); ok {
		s.clusterPrefix = prefix
	} else {
		s.clusterPrefix = "k8test"
	}

	path := fmt.Sprintf("%s/cluster/admin.conf", k8test)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read kubeconfig: %s\n", err)
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		log.Fatalf("failed to apply kubeconfig %s: %s\n", path, err)
	}

	s.k8s, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("failed to spawn kubernetes client: %s\n", err)
	}

	// Cloudscale client
	token, ok := os.LookupEnv("CLOUDSCALE_API_TOKEN")
	if !ok {
		log.Fatal("could not find CLOUDSCALE_API_TOKEN environment variable\n")
	}

	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token,
	})

	httpClient := oauth2.NewClient(context.Background(), tokenSource)
	httpClient.Timeout = 10 * time.Second

	s.api = cloudscale.NewClient(httpClient)
}

func (s *IntegrationTestSuite) SetupTest() {
	s.ns = fmt.Sprintf("cloudscale-test-%08x", rand.Uint32())

	_, err := s.k8s.CoreV1().Namespaces().Create(
		context.Background(),
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: s.ns,
			},
		},
		metav1.CreateOptions{},
	)

	if err != nil {
		panic(fmt.Sprintf("could not create namespace %s: %s", s.ns, err))
	}
}

func (s *IntegrationTestSuite) Region() string {
	return s.Nodes()[0].Labels["topology.kubernetes.io/region"]
}

func (s *IntegrationTestSuite) CreateGlobalFloatingIP() (
	*cloudscale.FloatingIP, error) {

	ip, err := s.api.FloatingIPs.Create(
		context.Background(), &cloudscale.FloatingIPCreateRequest{
			IPVersion: 4,
			Type:      "global",
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create Floating IP: %s", err)
	}

	s.resources = append(s.resources, ip.HREF)

	return ip, nil
}

func (s *IntegrationTestSuite) CreateRegionalFloatingIP(region string) (
	*cloudscale.FloatingIP, error) {

	ip, err := s.api.FloatingIPs.Create(
		context.Background(), &cloudscale.FloatingIPCreateRequest{
			IPVersion: 4,
			Type:      "regional",
			RegionalResourceRequest: cloudscale.RegionalResourceRequest{
				Region: region,
			},
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create Floating IP: %s", err)
	}

	s.resources = append(s.resources, ip.HREF)

	return ip, nil
}

func (s *IntegrationTestSuite) TearDownTest() {
	errors := 0

	if s.resources != nil {
		for _, url := range s.resources {
			req, err := s.api.NewRequest(
				context.Background(), http.MethodDelete, url, nil)
			if err != nil {
				s.T().Logf("preparing to delete %s failed: %s", url, err)
				errors++
			}

			err = s.api.Do(context.Background(), req, nil)
			if err != nil {
				s.T().Logf("deleting %s failed: %s", url, err)
				errors++
			}
		}
	}
	s.resources = nil

	err := s.k8s.CoreV1().Namespaces().Delete(
		context.Background(),
		s.ns,
		metav1.DeleteOptions{},
	)

	if err != nil {
		s.T().Logf("could not delete namespace %s: %s", s.ns, err)
		errors++
	}

	// Wait up to two minutes for the namespace to be deleted
	timeout := 120 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err = wait.PollUntilContextCancel(ctx, 1*time.Second, true,
		func(ctx context.Context) (bool, error) {
			_, err := s.k8s.CoreV1().Namespaces().Get(
				ctx,
				s.ns,
				metav1.GetOptions{},
			)

			// Not found, we are done
			if k8serrors.IsNotFound(err) {
				return true, nil
			}

			// Another error, fail
			if err != nil {
				return false, err
			}

			// Found, try again
			return false, nil
		})

	if err != nil {
		s.T().Logf("took too long to delete namespace %s: %s", s.ns, err)
		errors++
	}

	if errors > 0 {
		panic(fmt.Sprintf("failed cleanup test: %d errors", errors))
	}

	s.ns = ""
}
