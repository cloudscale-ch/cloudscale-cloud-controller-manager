package cloudscale_ccm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// serviceInfo wraps v1.Service with cloudscale specific methods
type serviceInfo struct {
	Service     *v1.Service
	clusterName string
}

func newServiceInfo(service *v1.Service, clusterName string) *serviceInfo {

	if service == nil {
		panic("v1.Service pointer is nil")
	}

	return &serviceInfo{Service: service, clusterName: clusterName}
}

// isSupported checks if the given service is one we care about. If we do
// not, false is returned, with an optional error message to give a hint
// about why we do not support it (may be ignored).
//
// This is due to the fact that Kubernetes might send a service our way, that
// is not handled by us.
func (s serviceInfo) isSupported() (bool, error) {

	// If you specify .spec.loadBalancerClass, it is assumed that a load
	// balancer implementation that matches the specified class is watching
	// for Services. Any default load balancer implementation (for example,
	// the one provided by the cloud provider) will ignore Services that have
	// this field set.
	//
	// https://kubernetes.io/docs/concepts/services-networking/service/#load-balancer-class
	if s.Service.Spec.LoadBalancerClass != nil {
		return false, fmt.Errorf(
			"not supported LoadBalancerClass: %s",
			*s.Service.Spec.LoadBalancerClass,
		)
	}

	return true, nil
}

// Returns the annotation for the given key (see LoadBalancer...), and will
// default to an empty string, unless some other default is specified.
//
// Warning: These defaults should not be changed going forward, as that would
// cause CCM to apply changes to existing clusters. If *really* necessary,
// use the LoadBalancerConfigVersion annotation stored on the service and
// add a new code path that accounts for this version when handing out
// defaults.
//
// Storing of all annotations on the service would be an alternative, but it
// would lead to excessive annotation usage, which should be avoided.
//
// Having a different code path for defaults vs. set values would make
// the code more complicated on the other hand.
//
// Not touching these defaults is therefore the simplest approach.
func (s serviceInfo) annotation(key string) string {
	switch key {
	case LoadBalancerConfigVersion:
		return "1"
	case LoadBalancerName:
		// Take the load balancer name or generate one
		return s.annotationOrElse(key, func() string {
			return fmt.Sprintf("k8s-service-%s", s.Service.UID)
		})
	case LoadBalancerZone:
		return s.annotationOrDefault(key, "")
	case LoadBalancerUUID:
		return s.annotationOrDefault(key, "")
	case LoadBalancerPoolProtocol:
		return s.annotationOrDefault(key, "tcp")
	case LoadBalancerFlavor:
		return s.annotationOrDefault(key, "lb-standard")
	case LoadBalancerPoolAlgorithm:
		return s.annotationOrDefault(key, "round_robin")
	case LoadBalancerHealthMonitorDelayS:
		return s.annotationOrDefault(key, "2")
	case LoadBalancerHealthMonitorTimeoutS:
		return s.annotationOrDefault(key, "1")
	case LoadBalancerHealthMonitorUpThreshold:
		return s.annotationOrDefault(key, "2")
	case LoadBalancerHealthMonitorDownThreshold:
		return s.annotationOrDefault(key, "3")
	case LoadBalancerHealthMonitorType:
		return s.annotationOrDefault(key, "tcp")
	case LoadBalancerHealthMonitorHTTP:
		return s.annotationOrDefault(key, "{}")
	case LoadBalancerListenerProtocol:
		return s.annotationOrDefault(key, "tcp")
	case LoadBalancerListenerAllowedCIDRs:
		return s.annotationOrDefault(key, "[]")
	case LoadBalancerListenerTimeoutClientDataMS:
		return s.annotationOrDefault(key, "50000")
	case LoadBalancerListenerTimeoutMemberConnectMS:
		return s.annotationOrDefault(key, "5000")
	case LoadBalancerListenerTimeoutMemberDataMS:
		return s.annotationOrDefault(key, "50000")
	default:
		return s.annotationOrElse(key, func() string {
			klog.Warning("unknown annotation:", key)
			return ""
		})
	}
}

// Returns the annotation as int, or an error
func (s serviceInfo) annotationInt(key string) (int, error) {
	v, err := strconv.Atoi(s.annotation(key))
	if err != nil {
		return 0, fmt.Errorf(
			"cannot convert %s to int (%s): %w",
			s.annotation(key),
			LoadBalancerHealthMonitorDelayS,
			err,
		)
	}
	return v, nil
}

// Returns the annotation as string list, or an error. The supported input
// format is JSON (e.g. `["foo", "bar"]`). An empty string is treated as
// an empty list.
func (s serviceInfo) annotationList(key string) ([]string, error) {
	value := s.annotation(key)

	if strings.Trim(value, " ") == "" {
		return make([]string, 0), nil
	}

	var list []string

	err := json.Unmarshal([]byte(value), &list)
	if err != nil {
		return nil, fmt.Errorf(
			"not a valid JSON string list: %s (%s): %w",
			value,
			LoadBalancerHealthMonitorDelayS,
			err,
		)
	}

	return list, nil
}

// annotationOrElase returns the annotation with the given key, or returns the
// result of the fallback function if the key does not exist.
func (s serviceInfo) annotationOrElse(key string, fn func() string) string {
	if s.Service.Annotations == nil {
		return fn()
	}

	value, ok := s.Service.Annotations[key]
	if !ok {
		return fn()
	}

	return value
}

// annotationOrDefault returns the annotation with the given key, or the
// default value if the key does not exist.
func (s serviceInfo) annotationOrDefault(key string, value string) string {
	return s.annotationOrElse(key, func() string { return value })
}

// annotateService takes a list of key/value pairs and applies them as
// annotations using JSON patch (https://jsonpatch.com/).
func (s serviceInfo) annotateService(
	ctx context.Context,
	k8s kubernetes.Interface,
	kv ...string,
) error {
	if len(kv) == 0 {
		return nil
	}

	if len(kv)%2 != 0 {
		return errors.New("expected an even number of arguments (key, value)")
	}

	if k8s == nil {
		return errors.New("no valid kubernetes client given")
	}

	operations := make([]map[string]any, 0, len(kv)/2)

	if s.Service.Annotations == nil {
		operations = append(operations,
			map[string]any{
				"op":    "add",
				"path":  "/metadata/annotations",
				"value": map[string]any{},
			},
		)
	}

	for ix := range kv {
		if ix%2 != 0 {
			continue
		}

		k := kv[ix]
		v := kv[ix+1]

		if s.Service.Annotations != nil && s.Service.Annotations[k] == v {
			continue
		}

		// https://www.rfc-editor.org/rfc/rfc6901#section-3
		k = strings.ReplaceAll(k, "~", "~0")
		k = strings.ReplaceAll(k, "/", "~1")

		path := fmt.Sprintf("/metadata/annotations/%s", k)

		operations = append(operations, map[string]any{
			"op":    "add",
			"path":  path,
			"value": v,
		})
	}

	if len(operations) == 0 {
		return nil
	}

	patch, err := json.Marshal(&operations)
	if err != nil {
		return fmt.Errorf("failed to encode patch operations: %w", err)
	}

	_, err = k8s.CoreV1().Services(s.Service.Namespace).Patch(
		ctx,
		s.Service.Name,
		types.JSONPatchType,
		patch,
		metav1.PatchOptions{},
	)

	if err != nil {
		return fmt.Errorf(
			"failed to apply patch to %s: %w", s.Service.Name, err)
	}

	return nil
}