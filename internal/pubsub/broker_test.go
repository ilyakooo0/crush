package pubsub

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestConcurrentShutdown ensures concurrent Shutdown calls don't panic on a
// double-close of the done channel.
func TestConcurrentShutdown(t *testing.T) {
	t.Parallel()
	b := NewBroker[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range 8 {
		b.Subscribe(ctx)
	}

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Shutdown()
		}()
	}
	wg.Wait()
}

// TestPublishMustDeliverDuringShutdown ensures sending outside the lock is
// safe when a subscriber channel is closed concurrently.
func TestPublishMustDeliverDuringShutdown(t *testing.T) {
	t.Parallel()
	b := NewBrokerWithOptions[int](1)
	b.SetMustDeliverTimeout(time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range 8 {
		b.Subscribe(ctx) // never drained, so buffers fill and sends block
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 50 {
			b.PublishMustDeliver(context.Background(), CreatedEvent, 1)
		}
	}()
	go func() {
		defer wg.Done()
		b.Shutdown()
	}()
	wg.Wait()
}

// TestSubscribeGoroutineExitsOnShutdown ensures the per-subscriber goroutine
// unblocks on Shutdown even when its context outlives the broker.
func TestSubscribeGoroutineExitsOnShutdown(t *testing.T) {
	t.Parallel()
	b := NewBroker[int]()
	// Background context: without the b.done wakeup this goroutine would
	// leak forever after Shutdown.
	ch := b.Subscribe(context.Background())
	b.Shutdown()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after Shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber channel not closed after Shutdown")
	}

	if got := b.GetSubscriberCount(); got != 0 {
		t.Fatalf("subscriber count = %d, want 0", got)
	}
}
