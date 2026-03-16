package broadcast

import (
	"sync"
	"testing"
)

func TestSubscribeBroadcastUnsubscribe(t *testing.T) {
	b := NewBroadcaster[int](1)
	ch := b.Subscribe()

	b.Broadcast(42)

	got := <-ch
	if got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}

	b.Unsubscribe(ch)

	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
}

func TestBroadcastNonBlockingDropsWhenFull(t *testing.T) {
	b := NewBroadcaster[int](1)
	ch := b.Subscribe()

	b.Broadcast(1)
	b.Broadcast(2)

	if gotLen := len(ch); gotLen != 1 {
		t.Fatalf("expected buffered length 1, got %d", gotLen)
	}

	got := <-ch
	if got != 1 {
		t.Fatalf("expected first event to remain, got %d", got)
	}
}

func TestBroadcastDisconnectsSlowSubscriber(t *testing.T) {
	b := NewBroadcaster[int](1)
	ch := b.Subscribe()

	b.Broadcast(1)
	b.Broadcast(2)

	_, ok := <-ch
	if !ok {
		t.Fatal("expected to read buffered event before close")
	}

	_, ok = <-ch
	if ok {
		t.Fatal("expected channel to be closed after slow subscriber disconnect")
	}
}

func TestConcurrentBroadcastUnsubscribeNoPanic(t *testing.T) {
	const subscribers = 20
	const broadcasts = 100

	b := NewBroadcaster[int](1)

	var wg sync.WaitGroup
	for i := range subscribers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch := b.Subscribe()
			// drain so we don't become slow subscriber
			go func() {
				for range ch {
				}
			}()
			if i%2 == 0 {
				b.Unsubscribe(ch)
			}
		}(i)
	}

	wg.Wait()

	var bwg sync.WaitGroup
	for range broadcasts {
		bwg.Add(1)
		go func() {
			defer bwg.Done()
			b.Broadcast(1)
		}()
	}
	bwg.Wait()
}

func TestSubscribeWithPredicateFiltersEvents(t *testing.T) {
	b := NewBroadcaster[int](2)
	ch := b.Subscribe(func(v int) bool { return v%2 == 0 })

	b.Broadcast(1)
	b.Broadcast(2)
	b.Broadcast(3)
	b.Broadcast(4)

	b.Unsubscribe(ch)

	var got []int
	for v := range ch {
		got = append(got, v)
	}
	if len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Fatalf("expected [2 4], got %v", got)
	}
}

func TestUnsubscribeUnknownChannelNoPanic(t *testing.T) {
	b := NewBroadcaster[int](1)
	ch := make(chan int, 1)

	b.Unsubscribe(ch)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic when sending to channel: %v", r)
		}
	}()

	ch <- 1
}
