package actions

import (
	"context"
	"testing"
	"time"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	"github.com/cloudscale-ch/cloudscale-go-sdk/v3"
	"github.com/stretchr/testify/assert"
)

func TestRefetch(t *testing.T) {
	assert.NotEmpty(t, Refetch().Label())

	v, err := Refetch().Run(context.Background(), nil)
	assert.Equal(t, Refresh, v)
	assert.NoError(t, err)
}

func TestCreateLbAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On("/v1/load-balancers", 201, "{}")
	server.Start()
	defer server.Close()

	action := CreateLb(&cloudscale.LoadBalancer{
		Name: "foo",
		Flavor: cloudscale.LoadBalancerFlavorStub{
			Slug: "lb-standard",
		},
		VIPAddresses: []cloudscale.VIPAddress{
			{Address: "10.0.0.1", Subnet: cloudscale.SubnetStub{
				CIDR: "10.0.0.1/24",
			}},
		},
	})

	assert.NotEmpty(t, action.Label())
	v, err := action.Run(context.Background(), server.Client())

	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerRequest
	server.LastSent(&sent)

	assert.Equal(t, "foo", sent.Name)
	assert.Equal(t, "lb-standard", sent.Flavor)
	assert.Equal(t, "10.0.0.1", (*sent.VIPAddresses)[0].Address)
}

func TestRenameLbAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On(
		"/v1/load-balancers/00000000-0000-0000-0000-000000000000", 204, "")

	server.Start()
	defer server.Close()

	action := RenameLb("00000000-0000-0000-0000-000000000000", "new-name")

	assert.NotEmpty(t, action.Label())
	v, err := action.Run(context.Background(), server.Client())

	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)
}

func TestAwaitLbAction(t *testing.T) {
	lb := cloudscale.LoadBalancer{}

	action := AwaitLb(&lb)
	assert.NotEmpty(t, action.Label())

	lb.Status = "changing"
	v, err := action.Run(context.Background(), nil)
	assert.NoError(t, err)
	assert.Equal(t, Refresh, v)

	lb.Status = "ready"
	v, err = action.Run(context.Background(), nil)
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)
}

func TestDeleteResourceAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On("/v1/foo", 204, "")
	server.On("/v1/bar", 403, "")
	server.Start()
	defer server.Close()

	action := DeleteResource("/v1/foo")
	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	action = DeleteResource("/v1/bar")

	v, err = action.Run(context.Background(), server.Client())
	assert.Error(t, err)
	assert.Equal(t, Errored, v)
}

func TestSleepAction(t *testing.T) {
	action := Sleep(100 * time.Millisecond)
	assert.NotEmpty(t, action.Label())

	start := time.Now()
	v, err := action.Run(context.Background(), nil)

	assert.Greater(t, time.Since(start), 100*time.Millisecond)
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start = time.Now()
	v, err = action.Run(ctx, nil)
	assert.Error(t, err)
	assert.Equal(t, Errored, v)
	assert.Greater(t, 1*time.Millisecond, time.Since(start))
}

func TestCreatePoolAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On("/v1/load-balancers/pools", 201, "{}")
	server.Start()
	defer server.Close()

	action := CreatePool("00000000-0000-0000-0000-000000000000",
		&cloudscale.LoadBalancerPool{
			Name:      "Foo",
			Algorithm: "round-robin",
			Protocol:  "tcp",
		},
	)

	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerPoolRequest
	server.LastSent(&sent)

	assert.Equal(t, "Foo", sent.Name)
	assert.Equal(t, "round-robin", sent.Algorithm)
	assert.Equal(t, "tcp", sent.Protocol)
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", sent.LoadBalancer)
}

func TestCreatePoolMemberAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On(
		"/v1/load-balancers/pools/00000000-0000-0000-0000-000000000000"+
			"/members", 201, "{}")
	server.Start()
	defer server.Close()

	action := CreatePoolMember("00000000-0000-0000-0000-000000000000",
		&cloudscale.LoadBalancerPoolMember{
			ProtocolPort: 80,
			MonitorPort:  8080,
			Address:      "10.0.0.1",
			Subnet: cloudscale.SubnetStub{
				UUID: "11111111-1111-1111-1111-111111111111",
			},
		},
	)

	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerPoolMemberRequest
	server.LastSent(&sent)

	assert.Equal(t, 80, sent.ProtocolPort)
	assert.Equal(t, 8080, sent.MonitorPort)
	assert.Equal(t, "10.0.0.1", sent.Address)
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", sent.Subnet)
}

func TestCreateListenerAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On("/v1/load-balancers/listeners", 201, "{}")
	server.Start()
	defer server.Close()

	action := CreateListener("00000000-0000-0000-0000-000000000000",
		&cloudscale.LoadBalancerListener{
			Name:                   "Foo",
			Protocol:               "tcp",
			ProtocolPort:           80,
			AllowedCIDRs:           []string{"10.0.0.0/24"},
			TimeoutClientDataMS:    1,
			TimeoutMemberConnectMS: 2,
			TimeoutMemberDataMS:    3,
		},
	)

	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerListenerRequest
	server.LastSent(&sent)

	assert.Equal(t, "00000000-0000-0000-0000-000000000000", sent.Pool)
	assert.Equal(t, "tcp", sent.Protocol)
	assert.Equal(t, 80, sent.ProtocolPort)
	assert.Equal(t, []string{"10.0.0.0/24"}, sent.AllowedCIDRs)
	assert.Equal(t, 1, sent.TimeoutClientDataMS)
	assert.Equal(t, 2, sent.TimeoutMemberConnectMS)
	assert.Equal(t, 3, sent.TimeoutMemberDataMS)
}

func TestUpdateListenerAllowedCIDRsAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On(
		"/v1/load-balancers/listeners/00000000-0000-0000-0000-000000000000",
		204, "")
	server.Start()
	defer server.Close()

	action := UpdateListenerAllowedCIDRs(
		"00000000-0000-0000-0000-000000000000", []string{"10.0.0.0/24"})

	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerListenerRequest
	server.LastSent(&sent)

	assert.Equal(t, []string{"10.0.0.0/24"}, sent.AllowedCIDRs)
}

func TestUpdateListenerTimeoutAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On(
		"/v1/load-balancers/listeners/00000000-0000-0000-0000-000000000000",
		204, "")
	server.Start()
	defer server.Close()

	// TimeoutClientDataMS
	action := UpdateListenerTimeout(
		"00000000-0000-0000-0000-000000000000",
		10,
		"client-data-ms",
	)
	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerListenerRequest
	server.LastSent(&sent)
	assert.Equal(t, 10, sent.TimeoutClientDataMS)

	// TimeoutMemberConnectMS
	action = UpdateListenerTimeout(
		"00000000-0000-0000-0000-000000000000",
		20,
		"member-connect-ms",
	)

	_, _ = action.Run(context.Background(), server.Client())
	server.LastSent(&sent)
	assert.Equal(t, 20, sent.TimeoutMemberConnectMS)

	// TimeoutMemberDataMS
	action = UpdateListenerTimeout(
		"00000000-0000-0000-0000-000000000000",
		30,
		"member-data-ms",
	)

	_, _ = action.Run(context.Background(), server.Client())
	server.LastSent(&sent)
	assert.Equal(t, 30, sent.TimeoutMemberDataMS)

	// Something unknown
	action = UpdateListenerTimeout(
		"00000000-0000-0000-0000-000000000000",
		30,
		"foo",
	)

	v, err = action.Run(context.Background(), server.Client())
	assert.Error(t, err)
	assert.Equal(t, Errored, v)
}

func TestCreateHealthMonitorAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On("/v1/load-balancers/health-monitors", 201, "{}")
	server.Start()
	defer server.Close()

	host := "foo"
	action := CreateHealthMonitor("00000000-0000-0000-0000-000000000000",
		&cloudscale.LoadBalancerHealthMonitor{
			DelayS:        1,
			TimeoutS:      2,
			UpThreshold:   3,
			DownThreshold: 4,
			Type:          "https",
			HTTP: &cloudscale.LoadBalancerHealthMonitorHTTP{
				ExpectedCodes: []string{"200"},
				Method:        "GET",
				UrlPath:       "/livez",
				Version:       "1.1",
				Host:          &host,
			},
		},
	)

	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerHealthMonitorRequest
	server.LastSent(&sent)

	assert.Equal(t, "00000000-0000-0000-0000-000000000000", sent.Pool)
	assert.Equal(t, 1, sent.DelayS)
	assert.Equal(t, 2, sent.TimeoutS)
	assert.Equal(t, 3, sent.UpThreshold)
	assert.Equal(t, 4, sent.DownThreshold)
	assert.Equal(t, "https", sent.Type)
	assert.Equal(t, []string{"200"}, sent.HTTP.ExpectedCodes)
	assert.Equal(t, "GET", sent.HTTP.Method)
	assert.Equal(t, "/livez", sent.HTTP.UrlPath)
	assert.Equal(t, "1.1", sent.HTTP.Version)
	assert.Equal(t, "foo", *sent.HTTP.Host)
}

func TestUpdateMonitorHTTPMethod(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On(
		"/v1/load-balancers/health-monitors"+
			"/00000000-0000-0000-0000-000000000000", 204, "")
	server.Start()
	defer server.Close()

	action := UpdateMonitorHTTPMethod(
		"00000000-0000-0000-0000-000000000000", "HEAD")

	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerHealthMonitorRequest
	server.LastSent(&sent)

	assert.Equal(t, "HEAD", sent.HTTP.Method)
}

func TestUpdateMonitorHTTPHost(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On(
		"/v1/load-balancers/health-monitors"+
			"/00000000-0000-0000-0000-000000000000", 204, "")
	server.Start()
	defer server.Close()

	host := "Foo"
	action := UpdateMonitorHTTPHost(
		"00000000-0000-0000-0000-000000000000", &host)

	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerHealthMonitorRequest
	server.LastSent(&sent)

	assert.Equal(t, "Foo", *sent.HTTP.Host)
}

func TestUpdateMonitorHTTPPath(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On(
		"/v1/load-balancers/health-monitors"+
			"/00000000-0000-0000-0000-000000000000", 204, "")
	server.Start()
	defer server.Close()

	action := UpdateMonitorHTTPPath(
		"00000000-0000-0000-0000-000000000000", "/foo")

	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerHealthMonitorRequest
	server.LastSent(&sent)

	assert.Equal(t, "/foo", sent.HTTP.UrlPath)
}

func TestUpdateMonitorHTTPExpectedCodes(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On(
		"/v1/load-balancers/health-monitors"+
			"/00000000-0000-0000-0000-000000000000", 204, "")
	server.Start()
	defer server.Close()

	action := UpdateMonitorHTTPExpectedCodes(
		"00000000-0000-0000-0000-000000000000", []string{"202"})

	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerHealthMonitorRequest
	server.LastSent(&sent)

	assert.Equal(t, []string{"202"}, sent.HTTP.ExpectedCodes)
}

func TestUpdateMonitorNumberAction(t *testing.T) {
	server := testkit.NewMockAPIServer()
	server.On(
		"/v1/load-balancers/health-monitors"+
			"/00000000-0000-0000-0000-000000000000", 204, "")
	server.Start()
	defer server.Close()

	// DelayS
	action := UpdateMonitorNumber(
		"00000000-0000-0000-0000-000000000000",
		1,
		"delay-s",
	)
	assert.NotEmpty(t, action.Label())

	v, err := action.Run(context.Background(), server.Client())
	assert.NoError(t, err)
	assert.Equal(t, Proceed, v)

	var sent cloudscale.LoadBalancerHealthMonitorRequest
	server.LastSent(&sent)

	assert.Equal(t, 1, sent.DelayS)

	// TimeoutS
	action = UpdateMonitorNumber(
		"00000000-0000-0000-0000-000000000000",
		1,
		"timeout-s",
	)

	_, _ = action.Run(context.Background(), server.Client())
	server.LastSent(&sent)
	assert.Equal(t, 1, sent.TimeoutS)

	// UpThreshold
	action = UpdateMonitorNumber(
		"00000000-0000-0000-0000-000000000000",
		1,
		"up-threshold",
	)

	_, _ = action.Run(context.Background(), server.Client())
	server.LastSent(&sent)
	assert.Equal(t, 1, sent.UpThreshold)

	// DownThreshold
	action = UpdateMonitorNumber(
		"00000000-0000-0000-0000-000000000000",
		1,
		"down-threshold",
	)

	_, _ = action.Run(context.Background(), server.Client())
	server.LastSent(&sent)
	assert.Equal(t, 1, sent.DownThreshold)

	// Something unknown
	action = UpdateMonitorNumber(
		"00000000-0000-0000-0000-000000000000",
		1,
		"foo",
	)

	v, err = action.Run(context.Background(), server.Client())
	assert.Error(t, err)
	assert.Equal(t, Errored, v)
}
