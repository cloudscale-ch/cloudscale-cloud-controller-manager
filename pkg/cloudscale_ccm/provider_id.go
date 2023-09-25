package cloudscale_ccm

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type cloudscaleProviderID struct {
	id uuid.UUID
}

func (i cloudscaleProviderID) String() string {
	return fmt.Sprintf("%s://%s", ProviderName, i.id.String())
}

func (i cloudscaleProviderID) UUID() uuid.UUID {
	return i.id
}

func parseCloudscaleProviderID(text string) (*cloudscaleProviderID, error) {
	if !strings.Contains(text, "://") {
		return nil, fmt.Errorf("bad providerID: %s has no '://'", text)
	}

	parts := strings.SplitN(text, "://", 2)
	parts[0] = strings.Trim(parts[0], " ")
	parts[1] = strings.Trim(parts[1], " ")

	if parts[0] == "" {
		return nil, fmt.Errorf("bad providerID: no provider in %s", text)
	}

	if parts[0] != ProviderName {
		return nil, fmt.Errorf("not a cloudscale providerID: %s", text)
	}

	if parts[1] == "" {
		return nil, fmt.Errorf("bad providerID: no id in %s", text)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, fmt.Errorf("bad providerID (no uuid at end): %s", text)
	}

	return &cloudscaleProviderID{
		id: id,
	}, nil
}
