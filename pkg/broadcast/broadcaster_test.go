package broadcast

import "testing"

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
