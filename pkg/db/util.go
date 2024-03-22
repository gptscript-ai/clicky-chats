package db

func expandSlice[T any](s []T, index int) []T {
	if len(s) > index {
		return s
	}
	return append(s, make([]T, index-len(s)+1)...)
}
