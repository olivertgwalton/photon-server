package domain

import (
	"fmt"
	"slices"
)

// Parse answers s as one of all, or an error naming what it was to be.
func Parse[T ~string](what, s string, all []T) (T, error) {
	if v := T(s); slices.Contains(all, v) {
		return v, nil
	}
	return "", fmt.Errorf("%s %q is not one of %v", what, s, all)
}
