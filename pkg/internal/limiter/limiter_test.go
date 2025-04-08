package limiter

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError(t *testing.T) {
	t.Parallel()

	lim := New(errors.New("fail"), "foo")

	v, err := lim.One()
	assert.Error(t, err)
	assert.Nil(t, v)

	_, err = lim.All()
	assert.Error(t, err)

	err = lim.None()
	assert.Error(t, err)
}

func TestFoundOne(t *testing.T) {
	t.Parallel()

	lim := New(nil, "foo")

	v, err := lim.One()
	assert.NoError(t, err)
	assert.Equal(t, "foo", *v)
}

func TestNotFoundOne(t *testing.T) {
	t.Parallel()

	lim := New[string](nil)

	v, err := lim.One()
	assert.Error(t, err)
	assert.Nil(t, v)
}

func TestAtMostOneEmpty(t *testing.T) {
	t.Parallel()

	lim := New[string](nil)

	v, err := lim.AtMostOne()
	assert.NoError(t, err)
	assert.Nil(t, v)
}

func TestAtMostOne(t *testing.T) {
	t.Parallel()

	lim := New(nil, "foo")

	v, err := lim.AtMostOne()
	assert.NoError(t, err)
	assert.Equal(t, "foo", *v)
}

func TestAtMostOneTooMany(t *testing.T) {
	t.Parallel()

	lim := New(nil, "foo", "bar")

	v, err := lim.AtMostOne()
	assert.Error(t, err)
	assert.Nil(t, v)
}

func TestNone(t *testing.T) {
	t.Parallel()

	lim := New[string](nil)
	assert.Nil(t, lim.None())
}

func TestNoneNotEmpty(t *testing.T) {
	t.Parallel()

	lim := New(nil, "foo")
	assert.Error(t, lim.None())
}

func TestAll(t *testing.T) {
	t.Parallel()

	lim := New(nil, "foo", "bar")

	v, err := lim.All()
	assert.NoError(t, err)
	assert.Equal(t, []string{"foo", "bar"}, v)
}
