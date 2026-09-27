package adapters

import "sync"

type Broker struct {
	mu        sync.Mutex
	listeners map[string]map[chan string]struct{}
}

func NewBroker() *Broker { return &Broker{listeners: make(map[string]map[chan string]struct{})} }
func (b *Broker) Publish(owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.listeners[owner] {
		select {
		case ch <- "changed":
		default:
		}
	}
}
func (b *Broker) Subscribe(owner string) (<-chan string, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listeners[owner] == nil {
		b.listeners[owner] = make(map[chan string]struct{})
	}
	ch := make(chan string, 1)
	b.listeners[owner][ch] = struct{}{}
	return ch, func() { b.mu.Lock(); defer b.mu.Unlock(); delete(b.listeners[owner], ch); close(ch) }
}
