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

func TestUnsubscribeMiddleElementSwapsWithLast(t *testing.T) {
	b := NewBroadcaster[int](1)
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	ch3 := b.Subscribe()

	b.Unsubscribe(ch2)

	b.Broadcast(99)

	got1, ok1 := <-ch1
	got3, ok3 := <-ch3

	if !ok1 || got1 != 99 {
		t.Fatalf("ch1: expected (99, true), got (%d, %v)", got1, ok1)
	}
	if !ok3 || got3 != 99 {
		t.Fatalf("ch3: expected (99, true), got (%d, %v)", got3, ok3)
	}

	select {
	case _, ok := <-ch2:
		if ok {
			t.Fatal("ch2 should be closed after unsubscribe")
		}
	default:
		t.Fatal("ch2 should be closed (readable) after unsubscribe")
	}

	b.Unsubscribe(ch1)
	b.Unsubscribe(ch3)
}

func TestUnsubscribeLastElement(t *testing.T) {
	b := NewBroadcaster[int](1)
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.Unsubscribe(ch2)

	b.Broadcast(7)

	got, ok := <-ch1
	if !ok || got != 7 {
		t.Fatalf("ch1: expected (7, true), got (%d, %v)", got, ok)
	}

	b.Unsubscribe(ch1)
}

func TestUnsubscribeFirstElement(t *testing.T) {
	b := NewBroadcaster[int](1)
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	ch3 := b.Subscribe()

	b.Unsubscribe(ch1)

	b.Broadcast(5)

	got2, ok2 := <-ch2
	got3, ok3 := <-ch3

	if !ok2 || got2 != 5 {
		t.Fatalf("ch2: expected (5, true), got (%d, %v)", got2, ok2)
	}
	if !ok3 || got3 != 5 {
		t.Fatalf("ch3: expected (5, true), got (%d, %v)", got3, ok3)
	}

	b.Unsubscribe(ch2)
	b.Unsubscribe(ch3)
}

func TestBroadcastMultipleSlowSubscribersRemovedInOneLock(t *testing.T) {
	b := NewBroadcaster[int](1)

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	ch3 := b.Subscribe()

	b.Broadcast(1)

	normalCh := b.Subscribe()

	b.Broadcast(2)

	got, ok := <-normalCh
	if !ok || got != 2 {
		t.Fatalf("normal subscriber: expected (2, true), got (%d, %v)", got, ok)
	}

	for i, ch := range []chan int{ch1, ch2, ch3} {
		for range ch {
		}
		if _, stillOpen := <-ch; stillOpen {
			t.Fatalf("slow[%d] should be closed after being evicted", i)
		}
	}

	b.Unsubscribe(normalCh)
}

func TestBroadcastNoSubscribers(t *testing.T) {
	b := NewBroadcaster[int](1)
	b.Broadcast(1)
}

func TestSubscribeNilPredicateIgnored(t *testing.T) {
	b := NewBroadcaster[int](2)
	ch := b.Subscribe(nil, func(v int) bool { return v > 0 }, nil)

	b.Broadcast(-1)
	b.Broadcast(1)
	b.Unsubscribe(ch)

	var got []int
	for v := range ch {
		got = append(got, v)
	}
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected [1], got %v", got)
	}
}
