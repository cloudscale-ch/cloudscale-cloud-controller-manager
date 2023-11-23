package actions

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v4"
)

type Action interface {
	Label() string
	Run(ctx context.Context, client *cloudscale.Client) (Control, error)
}

// RefetchAction is an empty action that sends a `Refresh` control code
type RefetchAction struct{}

func Refetch() Action {
	return &RefetchAction{}
}

func (a *RefetchAction) Label() string {
	return "refetch"
}

func (a *RefetchAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	return Refresh, nil
}

// CreateLbAction allows to create a load balancer that does not exist yet,
// using a fully speced load balancer instance.
type CreateLbAction struct {
	lb *cloudscale.LoadBalancer
}

func CreateLb(lb *cloudscale.LoadBalancer) Action {
	return &CreateLbAction{lb: lb}
}

func (a *CreateLbAction) Label() string {
	return fmt.Sprintf("create-lb(%s)", a.lb.Name)
}

func (a *CreateLbAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	addrs := make([]cloudscale.VIPAddressRequest, 0, len(a.lb.VIPAddresses))
	for _, addr := range a.lb.VIPAddresses {
		addrs = append(addrs, cloudscale.VIPAddressRequest{
			Address: addr.Address,
			Subnet:  addr.Subnet.UUID,
		})
	}

	_, err := client.LoadBalancers.Create(ctx, &cloudscale.LoadBalancerRequest{
		Name:         a.lb.Name,
		Flavor:       a.lb.Flavor.Slug,
		VIPAddresses: &addrs,
		ZonalResourceRequest: cloudscale.ZonalResourceRequest{
			Zone: a.lb.Zone.Slug,
		},
	})

	return ProceedOnSuccess(err)
}

// RenameLbAction allows to rename a load balancer via UUID
type RenameLbAction struct {
	UUID string
	Name string
}

func RenameLb(uuid string, name string) Action {
	return &RenameLbAction{UUID: uuid, Name: name}
}

func (a *RenameLbAction) Label() string {
	return fmt.Sprintf("rename-lb(%s -> %s)", a.UUID, a.Name)
}

func (a *RenameLbAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	return ProceedOnSuccess(client.LoadBalancers.Update(ctx, a.UUID,
		&cloudscale.LoadBalancerRequest{
			Name: a.Name,
		},
	))
}

// AwaitLbAction waits for a load balancer to be ready
type AwaitLbAction struct {
	lb *cloudscale.LoadBalancer
}

func AwaitLb(lb *cloudscale.LoadBalancer) Action {
	return &AwaitLbAction{lb: lb}
}

func (a *AwaitLbAction) Label() string {
	return fmt.Sprintf(
		"await-lb(%s is %s)", a.lb.Name, a.lb.Status)
}

func (a *AwaitLbAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	// Abort if there are states we cannot continue with
	switch a.lb.Status {
	case "changing":
		return Refresh, nil
	default:
		return Proceed, nil
	}
}

// DeleteMonitorsAction deletes the given resources
type DeleteResourceAction struct {
	url string
}

func DeleteResource(url string) Action {
	return &DeleteResourceAction{url: url}
}

func (a *DeleteResourceAction) Label() string {
	return fmt.Sprintf("delete-resource(%s)", a.url)
}

func (a *DeleteResourceAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	req, err := client.NewRequest(ctx, http.MethodDelete, a.url, nil)
	if err != nil {
		return Errored, fmt.Errorf(
			"delete resource action for %s failed: %w", a.url, err)
	}

	return ProceedOnSuccess(client.Do(ctx, req, nil))
}

// SleepAction sleeps for a given amount of time, unless cancelled
type SleepAction struct {
	duration time.Duration
}

func Sleep(duration time.Duration) Action {
	return &SleepAction{duration: duration}
}

func (a *SleepAction) Label() string {
	return fmt.Sprintf("sleep-%s", a.duration)
}

func (a *SleepAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {
	select {
	case <-ctx.Done():
		return Errored, fmt.Errorf("action has been aborted")
	case <-time.After(a.duration):
		break
	}

	return Proceed, nil
}

// CreatePoolAction creates a pool
type CreatePoolAction struct {
	lbUUID string
	pool   *cloudscale.LoadBalancerPool
}

func CreatePool(lbUUID string, pool *cloudscale.LoadBalancerPool) Action {
	return &CreatePoolAction{lbUUID: lbUUID, pool: pool}
}

func (a *CreatePoolAction) Label() string {
	return fmt.Sprintf("create-pool(%s/%s)", a.lbUUID, a.pool.Name)
}

func (a *CreatePoolAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	_, err := client.LoadBalancerPools.Create(ctx,
		&cloudscale.LoadBalancerPoolRequest{
			Name:         a.pool.Name,
			LoadBalancer: a.lbUUID,
			Algorithm:    a.pool.Algorithm,
			Protocol:     a.pool.Protocol,
		},
	)

	return ProceedOnSuccess(err)
}

// CreaetPoolMemberAction creates a pool member
type CreatePoolMemberAction struct {
	poolUUID string
	member   *cloudscale.LoadBalancerPoolMember
}

func CreatePoolMember(
	poolUUID string, member *cloudscale.LoadBalancerPoolMember) Action {

	return &CreatePoolMemberAction{poolUUID: poolUUID, member: member}
}

func (a *CreatePoolMemberAction) Label() string {
	return fmt.Sprintf("create-pool-member(%s/%s)", a.poolUUID, a.member.Name)
}

func (a *CreatePoolMemberAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	_, err := client.LoadBalancerPoolMembers.Create(ctx, a.poolUUID,
		&cloudscale.LoadBalancerPoolMemberRequest{
			Name:         a.member.Name,
			ProtocolPort: a.member.ProtocolPort,
			MonitorPort:  a.member.MonitorPort,
			Address:      a.member.Address,
			Subnet:       a.member.Subnet.UUID,
		},
	)

	return ProceedOnSuccess(err)
}

// CreateListenerAction creates a listener
type CreateListenerAction struct {
	poolUUID string
	listener *cloudscale.LoadBalancerListener
}

func CreateListener(
	poolUUID string, listener *cloudscale.LoadBalancerListener) Action {

	return &CreateListenerAction{poolUUID: poolUUID, listener: listener}
}

func (a *CreateListenerAction) Label() string {
	return fmt.Sprintf("create-listener(%s/%s)", a.poolUUID, a.listener.Name)
}

func (a *CreateListenerAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	_, err := client.LoadBalancerListeners.Create(ctx,
		&cloudscale.LoadBalancerListenerRequest{
			Pool:                   a.poolUUID,
			Name:                   a.listener.Name,
			Protocol:               a.listener.Protocol,
			ProtocolPort:           a.listener.ProtocolPort,
			AllowedCIDRs:           a.listener.AllowedCIDRs,
			TimeoutClientDataMS:    a.listener.TimeoutClientDataMS,
			TimeoutMemberConnectMS: a.listener.TimeoutMemberConnectMS,
			TimeoutMemberDataMS:    a.listener.TimeoutMemberDataMS,
		},
	)

	return ProceedOnSuccess(err)
}

// UpdateListenerAllowedCIDRsAction updates a listener's allowed CIDRs property
type UpdateListenerAllowedCIDRsAction struct {
	listenerUUID string
	allowedCIDRs []string
}

func UpdateListenerAllowedCIDRs(
	listenerUUID string, allowedCIDRs []string) Action {

	return &UpdateListenerAllowedCIDRsAction{
		listenerUUID: listenerUUID,
		allowedCIDRs: allowedCIDRs,
	}
}

func (a *UpdateListenerAllowedCIDRsAction) Label() string {
	return fmt.Sprintf("update-cidrs(%s/%s)",
		a.listenerUUID, strings.Join(a.allowedCIDRs, ","))
}

func (a *UpdateListenerAllowedCIDRsAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	err := client.LoadBalancerListeners.Update(ctx,
		a.listenerUUID,
		&cloudscale.LoadBalancerListenerRequest{
			AllowedCIDRs: a.allowedCIDRs,
		},
	)

	return ProceedOnSuccess(err)
}

// UpdateListenerTimeoutAction updates a listener's timeout
type UpdateListenerTimeoutAction struct {
	key          string
	listenerUUID string
	timeout      int
}

func UpdateListenerTimeout(
	listenerUUID string, timeout int, key string) Action {

	return &UpdateListenerTimeoutAction{
		listenerUUID: listenerUUID,
		timeout:      timeout,
		key:          key,
	}
}

func (a *UpdateListenerTimeoutAction) Label() string {
	return fmt.Sprintf("update-listener-timeout-%s(%s: %dms)",
		a.key, a.listenerUUID, a.timeout)
}

func (a *UpdateListenerTimeoutAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	req := cloudscale.LoadBalancerListenerRequest{}

	switch a.key {
	case "client-data-ms":
		req.TimeoutClientDataMS = a.timeout
	case "member-connect-ms":
		req.TimeoutMemberConnectMS = a.timeout
	case "member-data-ms":
		req.TimeoutMemberDataMS = a.timeout
	default:
		return Errored, fmt.Errorf("unknown timeout key: %s", a.key)

	}

	return ProceedOnSuccess(
		client.LoadBalancerListeners.Update(ctx, a.listenerUUID, &req))
}

// CreateHealthMonitorAction creates a health monitor
type CreateHealthMonitorAction struct {
	poolUUID string
	monitor  *cloudscale.LoadBalancerHealthMonitor
}

func CreateHealthMonitor(
	poolUUID string, monitor *cloudscale.LoadBalancerHealthMonitor) Action {

	return &CreateHealthMonitorAction{poolUUID: poolUUID, monitor: monitor}
}

func (a *CreateHealthMonitorAction) Label() string {
	return fmt.Sprintf("create-monitor(%s/%s)", a.poolUUID, a.monitor.Type)
}

func (a *CreateHealthMonitorAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	var http *cloudscale.LoadBalancerHealthMonitorHTTP
	if a.monitor.HTTP != nil {
		http = a.monitor.HTTP
	} else {
		http = &cloudscale.LoadBalancerHealthMonitorHTTP{}
	}

	_, err := client.LoadBalancerHealthMonitors.Create(ctx,
		&cloudscale.LoadBalancerHealthMonitorRequest{
			Pool:          a.poolUUID,
			DelayS:        a.monitor.DelayS,
			TimeoutS:      a.monitor.TimeoutS,
			UpThreshold:   a.monitor.UpThreshold,
			DownThreshold: a.monitor.DownThreshold,
			Type:          a.monitor.Type,
			HTTP: &cloudscale.LoadBalancerHealthMonitorHTTPRequest{
				Method:        http.Method,
				UrlPath:       http.UrlPath,
				Version:       http.Version,
				Host:          http.Host,
				ExpectedCodes: http.ExpectedCodes,
			},
		},
	)

	return ProceedOnSuccess(err)
}

// UpdateMonitorHTTPMethod updates a monitor's HTTP method
type UpdateMonitorHTTPMethodAction struct {
	monitorUUID string
	method      string
}

func UpdateMonitorHTTPMethod(monitorUUID string, method string) Action {
	return &UpdateMonitorHTTPMethodAction{
		monitorUUID: monitorUUID,
		method:      method,
	}
}

func (a *UpdateMonitorHTTPMethodAction) Label() string {
	return fmt.Sprintf(
		"update-monitor-http-method (%s: %s)", a.monitorUUID, a.method)
}

func (a *UpdateMonitorHTTPMethodAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	err := client.LoadBalancerHealthMonitors.Update(ctx, a.monitorUUID,
		&cloudscale.LoadBalancerHealthMonitorRequest{
			HTTP: &cloudscale.LoadBalancerHealthMonitorHTTPRequest{
				Method: a.method,
			},
		},
	)

	return ProceedOnSuccess(err)
}

// UpdateMonitorHTTPPath updates a monitor's HTTP path
type UpdateMonitorHTTPPathAction struct {
	monitorUUID string
	path        string
}

func UpdateMonitorHTTPPath(monitorUUID string, path string) Action {
	return &UpdateMonitorHTTPPathAction{
		monitorUUID: monitorUUID,
		path:        path,
	}
}

func (a *UpdateMonitorHTTPPathAction) Label() string {
	return fmt.Sprintf(
		"update-monitor-http-path (%s: %s)", a.monitorUUID, a.path)
}

func (a *UpdateMonitorHTTPPathAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	err := client.LoadBalancerHealthMonitors.Update(ctx, a.monitorUUID,
		&cloudscale.LoadBalancerHealthMonitorRequest{
			HTTP: &cloudscale.LoadBalancerHealthMonitorHTTPRequest{
				UrlPath: a.path,
			},
		},
	)

	return ProceedOnSuccess(err)
}

// UpdateMonitorHTTPHost updates a monitor's HTTP host
type UpdateMonitorHTTPHostAction struct {
	monitorUUID string
	host        *string
}

func UpdateMonitorHTTPHost(monitorUUID string, host *string) Action {
	return &UpdateMonitorHTTPHostAction{
		monitorUUID: monitorUUID,
		host:        host,
	}
}

func (a *UpdateMonitorHTTPHostAction) Label() string {
	return fmt.Sprintf(
		"update-monitor-http-host (%s: %v)", a.monitorUUID, a.host)
}

func (a *UpdateMonitorHTTPHostAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	err := client.LoadBalancerHealthMonitors.Update(ctx, a.monitorUUID,
		&cloudscale.LoadBalancerHealthMonitorRequest{
			HTTP: &cloudscale.LoadBalancerHealthMonitorHTTPRequest{
				Host: a.host,
			},
		},
	)

	return ProceedOnSuccess(err)
}

// UpdateMonitorHTTPExpectedCodes updates a monitor's HTTP expected codes
type UpdateMonitorHTTPExpectedCodesAction struct {
	monitorUUID   string
	expectedCodes []string
}

func UpdateMonitorHTTPExpectedCodes(
	monitorUUID string, expectedCodes []string) Action {

	return &UpdateMonitorHTTPExpectedCodesAction{
		monitorUUID:   monitorUUID,
		expectedCodes: expectedCodes,
	}
}

func (a *UpdateMonitorHTTPExpectedCodesAction) Label() string {
	return fmt.Sprintf(
		"update-monitor-http-expected-codes (%s: %s)",
		a.monitorUUID,
		a.expectedCodes,
	)
}

func (a *UpdateMonitorHTTPExpectedCodesAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	err := client.LoadBalancerHealthMonitors.Update(ctx, a.monitorUUID,
		&cloudscale.LoadBalancerHealthMonitorRequest{
			HTTP: &cloudscale.LoadBalancerHealthMonitorHTTPRequest{
				ExpectedCodes: a.expectedCodes,
			},
		},
	)

	return ProceedOnSuccess(err)
}

// UpdateMonitorNumberAction updates a monitor's numbers
type UpdateMonitorNumberAction struct {
	monitorUUID string
	number      int
	key         string
}

func UpdateMonitorNumber(monitorUUID string, number int, key string) Action {
	return &UpdateMonitorNumberAction{
		key:         key,
		monitorUUID: monitorUUID,
		number:      number,
	}
}

func (a *UpdateMonitorNumberAction) Label() string {
	return fmt.Sprintf("update-monitor-%s(%s: %d)",
		a.key, a.monitorUUID, a.number)
}

func (a *UpdateMonitorNumberAction) Run(
	ctx context.Context, client *cloudscale.Client) (Control, error) {

	req := cloudscale.LoadBalancerHealthMonitorRequest{}

	switch a.key {
	case "delay-s":
		req.DelayS = a.number
	case "timeout-s":
		req.TimeoutS = a.number
	case "up-threshold":
		req.UpThreshold = a.number
	case "down-threshold":
		req.DownThreshold = a.number
	default:
		return Errored, fmt.Errorf("unknown timeout key: %s", a.key)
	}

	return ProceedOnSuccess(
		client.LoadBalancerHealthMonitors.Update(ctx, a.monitorUUID, &req))
}
