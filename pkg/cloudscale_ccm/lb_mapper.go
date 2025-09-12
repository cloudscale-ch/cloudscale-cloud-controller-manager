package cloudscale_ccm

import (
	"context"
	"errors"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/limiter"
	cloudscale "github.com/cloudscale-ch/cloudscale-go-sdk/v6"
)

// lbMapper maps cloudscale loadbalancers to Kubernetes services.
type lbMapper struct {
	client *cloudscale.Client
}

// findByServiceInfo returns loadbalancers matching the given service info
// (there may be multiple matches).
func (l *lbMapper) findByServiceInfo(
	ctx context.Context,
	serviceInfo *serviceInfo,
) *limiter.Limiter[cloudscale.LoadBalancer] {

	// If we have a UUID, look for both the service and the UUID. Usually
	// we expect to only see one, but it is possible for the UUID to point
	// to another LB than the service name, in which case we return both
	// so the caller can decide if that is sane or not.
	if uuid := serviceInfo.annotation(LoadBalancerUUID); uuid != "" {
		return limiter.Join(
			l.getByUUID(ctx, uuid),
			l.findByName(ctx, serviceInfo.annotation(LoadBalancerName)),
		).Unique(
			func(a *cloudscale.LoadBalancer, b *cloudscale.LoadBalancer) bool {
				return a.UUID == b.UUID
			},
		)
	}

	return l.findByName(ctx, serviceInfo.annotation(LoadBalancerName))
}

func (l *lbMapper) getByUUID(
	ctx context.Context,
	uuid string,
) *limiter.Limiter[cloudscale.LoadBalancer] {

	server, err := l.client.LoadBalancers.Get(ctx, uuid)
	if err != nil {
		var response *cloudscale.ErrorResponse

		if errors.As(err, &response) && response.StatusCode == 404 {
			return limiter.New[cloudscale.LoadBalancer](nil)
		}

		return limiter.New[cloudscale.LoadBalancer](err)
	}

	return limiter.New[cloudscale.LoadBalancer](nil, *server)
}

// findByName returns loadbalancers matching the given name (there may be
// multiple matches).
func (l *lbMapper) findByName(
	ctx context.Context,
	name string,
) *limiter.Limiter[cloudscale.LoadBalancer] {

	if name == "" {
		return limiter.New[cloudscale.LoadBalancer](
			errors.New("no load balancer with empty name found"))
	}

	lbs, err := l.client.LoadBalancers.List(ctx)
	if err != nil {
		return limiter.New[cloudscale.LoadBalancer](err)
	}

	matches := []cloudscale.LoadBalancer{}
	for _, lb := range lbs {
		l := lb

		if l.Name == name {
			matches = append(matches, l)
		}
	}

	return limiter.New[cloudscale.LoadBalancer](nil, matches...)
}
