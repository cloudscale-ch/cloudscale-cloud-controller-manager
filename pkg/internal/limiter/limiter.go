package limiter

import (
	"errors"
)

// Limiter is used to wrap slice responses with functions to assert that
// an expected number of elements was found.
type Limiter[T any] struct {
	Error    error
	elements []T
}

func New[T any](err error, elements ...T) *Limiter[T] {
	return &Limiter[T]{
		Error:    err,
		elements: elements,
	}
}

// All returns the full set of answers.
func (t *Limiter[T]) All() ([]T, error) {
	if t.Error != nil {
		return nil, t.Error
	}

	return t.elements, nil
}

// One returns exactly One item, or an error.
func (t *Limiter[T]) One() (*T, error) {
	if t.Error != nil {
		return nil, t.Error
	}
	if len(t.elements) > 1 {
		return nil, errors.New("found more than one")
	}
	if len(t.elements) < 1 {
		return nil, errors.New("found none")
	}

	return &t.elements[0], nil
}

// None returns nil if there is no element, or an error.
func (t *Limiter[T]) None() error {
	if t.Error != nil {
		return t.Error
	}
	if len(t.elements) > 0 {
		return errors.New("found some elements")
	}

	return nil
}

// AtMostOne returns no item (nil) or one, or fails with an error.
func (t *Limiter[T]) AtMostOne() (*T, error) {
	if t.Error != nil {
		return nil, t.Error
	}
	if len(t.elements) > 1 {
		return nil, errors.New("found more than one")
	}
	if len(t.elements) < 1 {
		return nil, nil
	}

	return &t.elements[0], nil
}
