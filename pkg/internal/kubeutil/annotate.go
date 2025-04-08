package kubeutil

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
)

// AnnotateService takes a list of key/value pairs and applies them as
// annotations using JSON patch (https://jsonpatch.com/).
func AnnotateService(
	ctx context.Context,
	client kubernetes.Interface,
	service *v1.Service,
	kv ...string,
) error {
	if len(kv) == 0 {
		return nil
	}

	if len(kv)%2 != 0 {
		return errors.New("expected an even number of arguments (key, value)")
	}

	if client == nil {
		return errors.New("no valid kubernetes client given")
	}

	operations := make([]map[string]any, 0, len(kv)/2)

	if service.Annotations == nil {
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

		if service.Annotations != nil && service.Annotations[k] == v {
			continue
		}

		// https://www.rfc-editor.org/rfc/rfc6901#section-3
		k = strings.ReplaceAll(k, "~", "~0")
		k = strings.ReplaceAll(k, "/", "~1")

		path := "/metadata/annotations/" + k

		operations = append(operations, map[string]any{
			"op":    "add",
			"path":  path,
			"value": v,
		})
	}

	if len(operations) == 0 {
		return nil
	}

	return PatchService(ctx, client, service, operations)
}

// PatchServices applies the given patch operations on the given service.
func PatchService(
	ctx context.Context,
	client kubernetes.Interface,
	service *v1.Service,
	operations []map[string]any,
) error {

	patch, err := json.Marshal(&operations)
	if err != nil {
		return fmt.Errorf("failed to encode patch operations: %w", err)
	}

	_, err = client.CoreV1().Services(service.Namespace).Patch(
		ctx,
		service.Name,
		types.JSONPatchType,
		patch,
		metav1.PatchOptions{},
	)

	if err != nil {
		return fmt.Errorf(
			"failed to apply patch to %s: %w", service.Name, err)
	}

	return nil
}

// PatchServiceExternalTrafficPolicy patches the external traffic policy of
// the given service.
func PatchServiceExternalTrafficPolicy(
	ctx context.Context,
	client kubernetes.Interface,
	service *v1.Service,
	policy v1.ServiceExternalTrafficPolicy,
) error {

	if service.Spec.ExternalTrafficPolicy == policy {
		return nil
	}

	operations := []map[string]any{
		{
			"op":    "replace",
			"path":  "/spec/externalTrafficPolicy",
			"value": string(policy),
		},
	}

	return PatchService(ctx, client, service, operations)
}

// IsKubernetesReleaseOrNewer fetches the Kubernetes release and returns
// true if matches the given major.minor release, or is newer.
func IsKubernetesReleaseOrNewer(
	client kubernetes.Interface,
	major int,
	minor int,
) (bool, error) {
	release, err := client.Discovery().ServerVersion()
	if err != nil {
		return false, fmt.Errorf("failed to read kubernetes version: %w", err)
	}

	k8sMajor, err := strconv.Atoi(release.Major)
	if err != nil {
		return false, fmt.Errorf(
			"failed to convert '%s' to int: %w", release.Major, err)
	}

	k8sMinor, err := strconv.Atoi(release.Minor)
	if err != nil {
		return false, fmt.Errorf(
			"failed to convert '%s' to int: %w", release.Minor, err)
	}

	return k8sMajor > major || (k8sMajor == major && k8sMinor >= minor), nil
}
