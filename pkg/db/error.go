package db

import "fmt"

type InvalidTypeError struct {
	Expected, Got any
}

func (e InvalidTypeError) Error() string {
	return fmt.Sprintf("invalid type: expected %T, got %T", e.Expected, e.Got)
}
