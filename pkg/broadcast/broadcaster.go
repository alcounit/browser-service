package broadcast

import "sync"

type Broadcaster[T any] interface {
	Subscribe(predicate ...func(T) bool) chan T
	Unsubscribe(chan T)
	Broadcast(event T)
}

type subscription[T any] struct {
	ch        chan T
	predicate func(T) bool
}

type broadcaster[T any] struct {
	mu      sync.RWMutex
	clients map[chan T]subscription[T]
	bufSize int
}

func NewBroadcaster[T any](bufSize int) Broadcaster[T] {
	return &broadcaster[T]{
		clients: make(map[chan T]subscription[T]),
		bufSize: bufSize,
	}
}

func (b *broadcaster[T]) Subscribe(predicate ...func(T) bool) chan T {
	ch := make(chan T, b.bufSize)
	sub := subscription[T]{ch: ch}
	if len(predicate) > 0 {
		sub.predicate = predicate[0]
	}

	b.mu.Lock()
	b.clients[ch] = sub
	b.mu.Unlock()

	return ch
}

func (b *broadcaster[T]) Unsubscribe(ch chan T) {
	b.mu.Lock()
	_, ok := b.clients[ch]
	if ok {
		delete(b.clients, ch)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *broadcaster[T]) Broadcast(event T) {
	b.mu.RLock()
	var slow []chan T
	for ch, sub := range b.clients {
		if sub.predicate != nil && !sub.predicate(event) {
			continue
		}
		select {
		case ch <- event:
		default:
			slow = append(slow, ch)
		}
	}
	b.mu.RUnlock()

	for _, ch := range slow {
		b.Unsubscribe(ch)
	}
}
