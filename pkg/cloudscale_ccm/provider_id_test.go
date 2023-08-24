package cloudscale_ccm

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestFormatProviderID(t *testing.T) {
	assertFormat := func(id string, formatted string) {
		assert.Equal(t, formatted, cloudscaleProviderID{
			id: uuid.MustParse(id),
		}.String())
	}

	assertFormat(
		"08d56bfe-40d0-4c68-a915-54f846c28c9e",
		"cloudscale://08d56bfe-40d0-4c68-a915-54f846c28c9e",
	)
	assertFormat(
		"DAD649BDA5784C899D3B95C353F28DE5",
		"cloudscale://dad649bd-a578-4c89-9d3b-95c353f28de5",
	)
}

func TestParseProviderIDSuccess(t *testing.T) {
	assertParseResult := func(id string, expected cloudscaleProviderID) {
		i, err := parseCloudscaleProviderID(id)

		assert.NoError(t, err)
		assert.Equal(t, expected, *i)
		assert.Equal(t, expected.UUID(), i.UUID())
	}

	id := cloudscaleProviderID{
		uuid.MustParse("08d56bfe-40d0-4c68-a915-54f846c28c9e"),
	}

	assertParseResult("cloudscale://08d56bfe-40d0-4c68-a915-54f846c28c9e", id)
	assertParseResult("cloudscale://08d56bfe40d04c68a91554f846c28c9e", id)
	assertParseResult("cloudscale://08D56BFE40D04C68A91554F846C28C9E", id)
}

func TestParseProviderIDError(t *testing.T) {
	assertError := func(id string) {
		_, err := parseCloudscaleProviderID(id)
		assert.Error(t, err)
	}

	assertError("cloudscale")
	assertError("")
	assertError("cloudscale:/08d56bfe-40d0-4c68-a915-54f846c28c9e")
	assertError("://08d56bfe-40d0-4c68-a915-54f846c28c9e")
	assertError("cloudscale://")
	assertError("cloudscale://foo")
	assertError("bigcorp://08d56bfe-40d0-4c68-a915-54f846c28c9e")
	assertError("cloduscale://08d56bfe-40d0-4c68-a915-54f846c28c9e")
}
