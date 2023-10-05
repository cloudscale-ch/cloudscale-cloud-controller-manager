package cloudscale_ccm

import "fmt"

// limiter is used to wrap slice responses with functions to assert that
// an expected number of elements was found.
type limiter[T any] struct {
	Error    error
	elements []T
}

func newLimiter[T any](err error, elements ...T) *limiter[T] {
	return &limiter[T]{
		Error:    err,
		elements: elements,
	}
}

// all returns the full set of answers
func (t *limiter[T]) all() ([]T, error) {
	if t.Error != nil {
		return nil, t.Error
	}
	return t.elements, nil
}

// one returns exactly one item, or an error.
func (t *limiter[T]) one() (*T, error) {
	if t.Error != nil {
		return nil, t.Error
	}
	if len(t.elements) > 1 {
		return nil, fmt.Errorf("found more than one")
	}
	if len(t.elements) < 1 {
		return nil, fmt.Errorf("found none")
	}
	return &t.elements[0], nil
}

// none returns nil if there is no element, or an error
func (t *limiter[T]) none() error {
	if t.Error != nil {
		return t.Error
	}
	if len(t.elements) > 0 {
		return fmt.Errorf("found some elements")
	}
	return nil
}

// atMostOne returns no item (nil) or one, or fails with an error
func (t *limiter[T]) atMostOne() (*T, error) {
	if t.Error != nil {
		return nil, t.Error
	}
	if len(t.elements) > 1 {
		return nil, fmt.Errorf("found more than one")
	}
	if len(t.elements) < 1 {
		return nil, nil
	}
	return &t.elements[0], nil
}
