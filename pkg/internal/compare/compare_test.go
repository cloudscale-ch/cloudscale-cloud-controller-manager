package compare

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiff(t *testing.T) {

	type Package struct {
		Name    string
		Version string
	}

	desired := []Package{
		{Name: "Winamp", Version: "2.9"},
		{Name: "Firefox", Version: "Nightly"},
	}
	actual := []Package{
		{Name: "Winamp", Version: "2.8"},
		{Name: "Firefox", Version: "Nightly"},
	}

	del, add := Diff(desired, actual, func(p Package) string {
		return fmt.Sprintf(
			p.Name,
			p.Version,
		)
	})

	assert.Equal(t, []Package{{Name: "Winamp", Version: "2.8"}}, del)
	assert.Equal(t, []Package{{Name: "Winamp", Version: "2.9"}}, add)
}

func TestMatch(t *testing.T) {

	type Package struct {
		Name    string
		Version string
	}

	old := []Package{
		{Name: "Winamp", Version: "2.8"},
	}
	new := []Package{
		{Name: "Winamp", Version: "2.9"},
	}

	matches := Match(old, new, func(p Package) string {
		return fmt.Sprintf(
			p.Name,
		)
	})

	assert.Equal(t, [][]Package{
		{
			{Name: "Winamp", Version: "2.8"},
			{Name: "Winamp", Version: "2.9"},
		},
	}, matches)
}
