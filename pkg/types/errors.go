package types

type ErrThreadID string

func (e ErrThreadID) Error() string {
	return "invalid thread id: " + string(e)
}
