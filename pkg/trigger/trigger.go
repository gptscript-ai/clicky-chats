package trigger

import (
	"sync"
)

type Trigger interface {
	Kick(id string) chan struct{}
	Triggered() <-chan struct{}
	Ready(id string)
}

type trigger struct {
	syncNow      chan struct{}
	readySignals map[string]chan struct{}
	lock         *sync.Mutex
}

func New() Trigger {
	return &trigger{
		syncNow:      make(chan struct{}),
		readySignals: make(map[string]chan struct{}),
		lock:         new(sync.Mutex),
	}
}

// Kick will kick the chat completion runner to check for new requests.
// If the runner is already running, then this will do nothing.
// The returned channel will be closed when the runner has processed the request with the given ID.
func (t *trigger) Kick(id string) chan struct{} {
	t.lock.Lock()
	ready, ok := t.readySignals[id]
	if !ok {
		ready = make(chan struct{})
		t.readySignals[id] = ready
	}
	t.lock.Unlock()

	// Since syncNow is unbuffered, then the default statement here will ensure that we only sync if we are not already
	// expecting a sync.
	select {
	case t.syncNow <- struct{}{}:
	default:
	}

	return ready
}

// Ready will close the channel for the given ID, if it exists, signaling the runner has processed the request.
func (t *trigger) Ready(id string) {
	t.lock.Lock()
	ready, ok := t.readySignals[id]
	if ok {
		delete(t.readySignals, id)
		close(ready)
	}
	t.lock.Unlock()
}

// Triggered will return a channel that will be sent on when a trigger is requested.
func (t *trigger) Triggered() <-chan struct{} {
	return t.syncNow
}
