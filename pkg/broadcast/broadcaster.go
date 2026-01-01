package broadcast

import "sync"

type Broadcaster[T any] interface {
	Subscribe() chan T
	Unsubscribe(chan T)
	Broadcast(event T)
}

type broadcaster[T any] struct {
	mu      sync.RWMutex
	clients map[chan T]struct{}
	bufSize int
}

func NewBroadcaster[T any](bufSize int) Broadcaster[T] {
	return &broadcaster[T]{
		clients: make(map[chan T]struct{}),
		bufSize: bufSize,
	}
}

func (b *broadcaster[T]) Subscribe() chan T {
	ch := make(chan T, b.bufSize)

	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()

	return ch
}

func (b *broadcaster[T]) Unsubscribe(ch chan T) {
	b.mu.Lock()
	_, ok := b.clients[ch]
	if ok {
		delete(b.clients, ch)
	}
	b.mu.Unlock()

	if ok {
		close(ch)
	}
}

func (b *broadcaster[T]) Broadcast(event T) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.clients {
		select {
		case ch <- event:
		default:
		}
	}
}
