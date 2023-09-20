//go:build integration

package integration

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	cloudscale "github.com/cloudscale-ch/cloudscale-go-sdk/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/oauth2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestMain(m *testing.M) {
	exitStatus := m.Run()
	os.Exit(exitStatus)
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

type IntegrationTestSuite struct {
	suite.Suite
	k8s kubernetes.Interface
	api *cloudscale.Client
}

func (s *IntegrationTestSuite) SetupSuite() {
	// Kubernetes client
	k8test, ok := os.LookupEnv("K8TEST_PATH")
	if !ok {
		log.Fatalf("could not find K8TEST_PATH environment variable\n")
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

func (s *IntegrationTestSuite) Nodes() []v1.Node {
	nodes, err := s.k8s.CoreV1().Nodes().List(
		context.Background(),
		metav1.ListOptions{},
	)

	assert.NoError(s.T(), err)
	return nodes.Items
}

func (s *IntegrationTestSuite) NodeNamed(name string) *v1.Node {
	node, err := s.k8s.CoreV1().Nodes().Get(
		context.Background(), name, metav1.GetOptions{},
	)

	if err != nil && errors.IsNotFound(err) {
		return nil
	}

	assert.NoError(s.T(), err)
	return node
}

func (s *IntegrationTestSuite) NodesLabeled(selector string) []v1.Node {
	nodes, err := s.k8s.CoreV1().Nodes().List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: selector,
		},
	)

	assert.NoError(s.T(), err)
	return nodes.Items
}

func (s *IntegrationTestSuite) NodesFiltered(fn func(*v1.Node) bool) []v1.Node {
	nodes := s.Nodes()
	matches := make([]v1.Node, 0, len(nodes))

	for _, n := range nodes {
		if fn(&n) {
			matches = append(matches, n)
		}
	}

	return matches
}

func (s *IntegrationTestSuite) Servers() []cloudscale.Server {
	servers, err := s.api.Servers.List(
		context.Background(),
		cloudscale.WithTagFilter(
			cloudscale.TagMap{
				"source": "k8test",
			},
		),
	)
	assert.NoError(s.T(), err, "could not list servers")
	return servers
}

func (s *IntegrationTestSuite) ServerNamed(name string) *cloudscale.Server {
	for _, server := range s.Servers() {
		if server.Name == name {
			return &server
		}
	}

	return nil
}

func (s *IntegrationTestSuite) TestKubernetesReady() {

	// Make sure we have at least one control, and some workers
	controls := s.NodesLabeled("node-role.kubernetes.io/control-plane")
	assert.True(s.T(), len(controls) > 0, "no controls found")

	nodes := s.Nodes()
	assert.True(s.T(), len(nodes) > len(controls), "no nodes found")
}

func (s *IntegrationTestSuite) TestNodesInitialized() {

	// None of the nodes should be uninitailized (this taint is removed, once
	// the CCM has responded).
	nodes := s.NodesFiltered(func(n *v1.Node) bool {
		for _, t := range n.Spec.Taints {
			if t.Key == "node.cloudprovider.kubernetes.io/uninitialized" {
				return true
			}
		}
		return false
	})
	assert.True(s.T(), len(nodes) == 0, "found uninitialized nodes")

}

func (s *IntegrationTestSuite) TestNodeMetadata() {
	assertMetadata := func(server cloudscale.Server) {
		node := s.NodeNamed(server.Name)

		assert.NotNil(s.T(), server, "server name not found:", server.Name)
		assert.NotNil(s.T(), node, "node name not found:", server.Name)

		assert.Equal(s.T(),
			fmt.Sprintf("cloudscale://%s", server.UUID),
			string(node.Spec.ProviderID),
			"node has wrong provider id: %s", node.Name)

		assert.Equal(s.T(),
			server.Flavor.Slug,
			node.Labels["node.kubernetes.io/instance-type"],
			"node has wrong flavor: %s", node.Name)

		assert.Equal(s.T(),
			strings.Trim(server.Zone.Slug, "0123456789"),
			node.Labels["topology.kubernetes.io/region"],
			"node has wrong region: %s", node.Name)

		assert.Equal(s.T(),
			server.Zone.Slug,
			node.Labels["topology.kubernetes.io/zone"],
			"node has wrong zone: %s", node.Name)

		assert.Equal(s.T(),
			node.Status.Addresses[0],
			v1.NodeAddress{
				Type:    v1.NodeHostName,
				Address: server.Name,
			},
			"node has wrong hostname node-address: %s", node.Name)

		assert.Equal(s.T(),
			node.Status.Addresses[1],
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: server.Interfaces[0].Addresses[0].Address,
			},
			"node has wrong public ipv4 node-address: %s", node.Name)

		assert.Equal(s.T(),
			node.Status.Addresses[2],
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: server.Interfaces[0].Addresses[1].Address,
			},
			"node has wrong public ipv6 node-address: %s", node.Name)
	}

	for _, server := range s.Servers() {
		assertMetadata(server)
	}
}

func (s *IntegrationTestSuite) TestRestartServer() {
	shutdownNodes := func() []v1.Node {
		return s.NodesFiltered(func(n *v1.Node) bool {
			for _, t := range n.Spec.Taints {
				if t.Key == "node.cloudprovider.kubernetes.io/shutdown" {
					return true
				}
			}
			return false
		})
	}

	require.Len(s.T(), shutdownNodes(), 0, "no nodes may be shutdown yet")

	// Shutdown the server
	server := s.ServerNamed("k8test-worker-1")
	err := s.api.Servers.Stop(context.Background(), server.UUID)
	assert.NoError(s.T(), err, "could not stop server %s", server.Name)

	// Wait for that to propagate (this includes some time to wait for the
	// server to actually shutdown)
	start := time.Now()
	for time.Since(start) < (120 * time.Second) {
		if len(shutdownNodes()) == 1 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	assert.Len(s.T(), shutdownNodes(), 1, "no shutdown node found")

	// Start the server
	err = s.api.Servers.Start(context.Background(), server.UUID)
	assert.NoError(s.T(), err, "could not start server %s", server.Name)

	start = time.Now()
	for time.Since(start) < (120 * time.Second) {
		if len(shutdownNodes()) == 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	assert.Len(s.T(), shutdownNodes(), 0, "node not detected as started")
}
