package compare

// Diff returns a list of items to delete, and a list of items to create, by
// comparing a desired list to an actual list. To compare, a function that
// takes a single item, and returns a string key is used.
//
// The string key is used for a map, to avoid having to compare all desired
// items to all actual items. It also is easier to write a key function, than
// it is to write a comparison function.
func Diff[T any](
	desired []T,
	actual []T,
	key func(item T) string,
) (delete []T, create []T) {

	d := make(map[string]T)
	a := make(map[string]T)

	for _, i := range desired {
		d[key(i)] = i
	}

	for _, i := range actual {
		a[key(i)] = i
	}

	for k, i := range d {
		_, ok := a[k]

		if !ok {
			create = append(create, i)
		}
	}

	for k, i := range a {
		_, ok := d[k]

		if !ok {
			delete = append(delete, i)
		}
	}

	return delete, create
}

// Match returns a list of items that match (each item in the list is a
// tuple of matching items).
func Match[T any](as []T, bs []T, key func(item T) string) [][]T {
	keys := make(map[string]T)
	matches := make([][]T, 0)

	for _, b := range bs {
		keys[key(b)] = b
	}

	for _, a := range as {
		v, ok := keys[key(a)]

		if ok {
			matches = append(matches, []T{a, v})
		}
	}

	return matches
}
