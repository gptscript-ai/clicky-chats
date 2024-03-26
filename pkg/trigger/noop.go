package trigger

// NewNoop creates a noop trigger, which returns nil for all channels.
// Note that receiving or sending on these channels will block forever.
// This means that all places where this is used will essentially have a polling implementation.
func NewNoop() Trigger {
	return &noop{}
}

type noop struct{}

func (t *noop) Triggered() <-chan struct{} {
	return nil
}

func (t *noop) Kick(string) chan struct{} {
	return nil
}

func (t *noop) Ready(string) {}
