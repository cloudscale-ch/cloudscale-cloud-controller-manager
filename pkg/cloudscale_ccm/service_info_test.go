package cloudscale_ccm

import (
	"testing"

	"github.com/cloudscale-ch/cloudscale-cloud-controller-manager/pkg/internal/testkit"
	"github.com/stretchr/testify/assert"
)

func TestNewServicePanic(t *testing.T) {
	assert.Panics(t, func() {
		newServiceInfo(nil, "")
	})
}

func TestIsSupported(t *testing.T) {
	s := testkit.NewService("service").V1()
	supported, err := newServiceInfo(s, "").isSupported()
	assert.True(t, supported)
	assert.NoError(t, err)
}

func TestIsNotSupported(t *testing.T) {
	s := testkit.NewService("service").V1()

	class := "foo"
	s.Spec.LoadBalancerClass = &class

	supported, err := newServiceInfo(s, "").isSupported()
	assert.False(t, supported)
	assert.Error(t, err)
}

func TestAnnotation(t *testing.T) {
	s := testkit.NewService("service").V1()
	i := newServiceInfo(s, "")

	assert.Empty(t, i.annotation(LoadBalancerUUID))
	assert.Equal(t, i.annotation(LoadBalancerFlavor), "lb-standard")
	assert.Equal(t, i.annotation("foo"), "")

	s.Annotations = make(map[string]string)

	assert.Empty(t, i.annotation(LoadBalancerUUID))
	assert.Equal(t, i.annotation(LoadBalancerFlavor), "lb-standard")
	assert.Equal(t, i.annotation("foo"), "")

	s.Annotations[LoadBalancerUUID] = "1234"
	s.Annotations[LoadBalancerFlavor] = "strawberry"

	assert.Equal(t, i.annotation(LoadBalancerUUID), "1234")
	assert.Equal(t, i.annotation(LoadBalancerFlavor), "strawberry")
	assert.Equal(t, i.annotation("foo"), "")
}

func TestAnnotationInt(t *testing.T) {
	s := testkit.NewService("service").V1()
	i := newServiceInfo(s, "")

	s.Annotations = make(map[string]string)
	s.Annotations["foo"] = "1"
	s.Annotations["bar"] = "a"

	v, err := i.annotationInt("foo")
	assert.Equal(t, v, 1)
	assert.NoError(t, err)

	v, err = i.annotationInt("bar")
	assert.Equal(t, v, 0)
	assert.Error(t, err)

	v, err = i.annotationInt("missing")
	assert.Equal(t, v, 0)
	assert.Error(t, err)
}

func TestAnnotationList(t *testing.T) {
	s := testkit.NewService("service").V1()
	i := newServiceInfo(s, "")

	s.Annotations = make(map[string]string)
	s.Annotations["foo"] = ""
	s.Annotations["bar"] = "[]"
	s.Annotations["baz"] = `["foo", "bar"]`
	s.Annotations["qux"] = `["f...`

	v, err := i.annotationList("foo")
	assert.Equal(t, v, []string{})
	assert.NoError(t, err)

	v, err = i.annotationList("bar")
	assert.Equal(t, v, []string{})
	assert.NoError(t, err)

	v, err = i.annotationList("baz")
	assert.Equal(t, v, []string{"foo", "bar"})
	assert.NoError(t, err)

	var empty []string
	v, err = i.annotationList("qux")
	assert.Equal(t, v, empty)
	assert.Error(t, err)
}
