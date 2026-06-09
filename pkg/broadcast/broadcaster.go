package broadcast

import "sync"

type Broadcaster[T any] interface {
	Subscribe(predicates ...func(T) bool) chan T
	Unsubscribe(chan T)
	Broadcast(event T)
}

type subscription[T any] struct {
	ch         chan T
	predicates []func(T) bool
}

func (s subscription[T]) matches(event T) bool {
	for _, p := range s.predicates {
		if !p(event) {
			return false
		}
	}
	return true
}

type broadcaster[T any] struct {
	mu      sync.RWMutex
	subs    []subscription[T]
	idx     map[chan T]int
	bufSize int
}

func NewBroadcaster[T any](bufSize int) Broadcaster[T] {
	return &broadcaster[T]{
		idx:     make(map[chan T]int),
		bufSize: bufSize,
	}
}

func (b *broadcaster[T]) Subscribe(predicates ...func(T) bool) chan T {
	ch := make(chan T, b.bufSize)
	var active []func(T) bool
	for _, p := range predicates {
		if p != nil {
			active = append(active, p)
		}
	}
	b.mu.Lock()
	b.idx[ch] = len(b.subs)
	b.subs = append(b.subs, subscription[T]{ch: ch, predicates: active})
	b.mu.Unlock()
	return ch
}

func (b *broadcaster[T]) remove(ch chan T) {
	i, ok := b.idx[ch]
	if !ok {
		return
	}
	last := len(b.subs) - 1
	if i != last {
		b.subs[i] = b.subs[last]
		b.idx[b.subs[i].ch] = i
	}
	b.subs[last] = subscription[T]{}
	b.subs = b.subs[:last]
	delete(b.idx, ch)
	close(ch)
}

func (b *broadcaster[T]) Unsubscribe(ch chan T) {
	b.mu.Lock()
	b.remove(ch)
	b.mu.Unlock()
}

func (b *broadcaster[T]) Broadcast(event T) {
	b.mu.RLock()
	var slow []chan T
	for _, sub := range b.subs {
		if !sub.matches(event) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			slow = append(slow, sub.ch)
		}
	}
	b.mu.RUnlock()

	if len(slow) == 0 {
		return
	}
	b.mu.Lock()
	for _, ch := range slow {
		b.remove(ch)
	}
	b.mu.Unlock()
}
