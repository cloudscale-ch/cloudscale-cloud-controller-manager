package cloudscale_ccm

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError(t *testing.T) {
	lim := newLimiter[string](errors.New("fail"), "foo")

	v, err := lim.one()
	assert.Error(t, err)
	assert.Nil(t, v)

	_, err = lim.all()
	assert.Error(t, err)

	err = lim.none()
	assert.Error(t, err)
}

func TestFoundOne(t *testing.T) {
	lim := newLimiter[string](nil, "foo")

	v, err := lim.one()
	assert.NoError(t, err)
	assert.Equal(t, "foo", *v)
}

func TestNotFoundOne(t *testing.T) {
	lim := newLimiter[string](nil)

	v, err := lim.one()
	assert.Error(t, err)
	assert.Nil(t, v)
}

func TestAtMostOneEmpty(t *testing.T) {
	lim := newLimiter[string](nil)

	v, err := lim.atMostOne()
	assert.NoError(t, err)
	assert.Nil(t, v)
}

func TestAtMostOne(t *testing.T) {
	lim := newLimiter[string](nil, "foo")

	v, err := lim.atMostOne()
	assert.NoError(t, err)
	assert.Equal(t, "foo", *v)
}

func TestAtMostOneTooMany(t *testing.T) {
	lim := newLimiter[string](nil, "foo", "bar")

	v, err := lim.atMostOne()
	assert.Error(t, err)
	assert.Nil(t, v)
}

func TestNone(t *testing.T) {
	lim := newLimiter[string](nil)
	assert.Nil(t, lim.none())
}

func TestNoneNotEmpty(t *testing.T) {
	lim := newLimiter[string](nil, "foo")
	assert.Error(t, lim.none())
}

func TestAll(t *testing.T) {
	lim := newLimiter[string](nil, "foo", "bar")

	v, err := lim.all()
	assert.NoError(t, err)
	assert.Equal(t, []string{"foo", "bar"}, v)
}
