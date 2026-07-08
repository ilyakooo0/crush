// Package pubsub provides a lightweight in-process broker for fan-out
// event delivery between services and the UI.
//
// Delivery semantics:
//
//   - [Broker.Publish] is best-effort and lossy under contention. If a
//     subscriber's channel is full, the event is dropped for that
//     subscriber, a warning is logged, and a counter is incremented.
//     This is the right choice for high-frequency intermediate updates
//     (e.g. streaming token deltas) where only the latest state
//     matters.
//
//   - [Broker.PublishMustDeliver] is bounded-blocking. For each
//     subscriber it first tries a non-blocking send, then falls back to
//     a per-subscriber blocking send with a hard timeout. On timeout the
//     event is dropped for that subscriber, an error is logged, and the
//     must-deliver drop counter is incremented. The publisher never
//     blocks indefinitely. This is the right choice for terminal events
//     (finish, tool result, error, cancel) that must not be silently
//     coalesced away.
//
// Drop counters ([Broker.DropCount], [Broker.MustDeliverDropCount]) are
// exposed so callers can surface saturation in telemetry.
package pubsub

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// bufferSize is the per-subscriber channel capacity for any broker
	// created via NewBroker. Publish is non-blocking, so a full buffer
	// drops events (with a warning log); sized to cover a long
	// streaming assistant turn (~one UpdatedEvent per token) even under
	// TUI render stalls.
	bufferSize = 4096

	// defaultMustDeliverTimeout is the per-subscriber upper bound on how
	// long [Broker.PublishMustDeliver] will block waiting for buffer
	// space before giving up on that subscriber.
	defaultMustDeliverTimeout = 50 * time.Millisecond
)

type Broker[T any] struct {
	subs                 map[chan Event[T]]struct{}
	mu                   sync.RWMutex
	done                 chan struct{}
	subCount             int
	channelBufferSize    int
	mustDeliverTimeout   time.Duration
	dropCount            atomic.Uint64
	mustDeliverDropCount atomic.Uint64
}

func NewBroker[T any]() *Broker[T] {
	return NewBrokerWithOptions[T](bufferSize)
}

func NewBrokerWithOptions[T any](channelBufferSize int) *Broker[T] {
	return &Broker[T]{
		subs:               make(map[chan Event[T]]struct{}),
		done:               make(chan struct{}),
		channelBufferSize:  channelBufferSize,
		mustDeliverTimeout: defaultMustDeliverTimeout,
	}
}

// SetMustDeliverTimeout overrides the per-subscriber timeout used by
// [Broker.PublishMustDeliver]. A zero or negative value resets to the
// default. Intended primarily for tests.
func (b *Broker[T]) SetMustDeliverTimeout(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if d <= 0 {
		b.mustDeliverTimeout = defaultMustDeliverTimeout
		return
	}
	b.mustDeliverTimeout = d
}

func (b *Broker[T]) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Close b.done under the lock so concurrent Shutdown calls (and the
	// per-subscriber goroutines, which also close under the lock) cannot
	// double-close it and panic.
	select {
	case <-b.done: // Already closed
		return
	default:
		close(b.done)
	}

	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}

	b.subCount = 0
}

func (b *Broker[T]) Subscribe(ctx context.Context) <-chan Event[T] {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.done:
		ch := make(chan Event[T])
		close(ch)
		return ch
	default:
	}

	sub := make(chan Event[T], b.channelBufferSize)
	b.subs[sub] = struct{}{}
	b.subCount++

	go func() {
		// Wake on either the subscriber's context ending or the broker
		// shutting down. Without the b.done case, a subscriber with a
		// long-lived context would park here forever after Shutdown,
		// leaking the goroutine.
		select {
		case <-ctx.Done():
		case <-b.done:
		}

		b.mu.Lock()
		defer b.mu.Unlock()

		// If the broker shut down, it already closed this channel under
		// the lock; don't close it again.
		select {
		case <-b.done:
			return
		default:
		}

		delete(b.subs, sub)
		close(sub)
		b.subCount--
	}()

	return sub
}

func (b *Broker[T]) GetSubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.subCount
}

// DropCount returns the cumulative number of events dropped by
// [Broker.Publish] because a subscriber's channel was full.
func (b *Broker[T]) DropCount() uint64 {
	return b.dropCount.Load()
}

// MustDeliverDropCount returns the cumulative number of events dropped
// by [Broker.PublishMustDeliver] after the per-subscriber timeout
// expired.
func (b *Broker[T]) MustDeliverDropCount() uint64 {
	return b.mustDeliverDropCount.Load()
}

// Publish delivers an event to every active subscriber.
//
// Delivery is non-blocking and lossy: if a subscriber's channel is full
// the event is dropped for that subscriber, a warning is logged, and
// [Broker.DropCount] is incremented. Use [Broker.PublishMustDeliver]
// for events that must not be silently dropped.
func (b *Broker[T]) Publish(t EventType, payload T) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	select {
	case <-b.done:
		return
	default:
	}

	event := Event[T]{Type: t, Payload: payload}

	for sub := range b.subs {
		select {
		case sub <- event:
		default:
			// Channel is full, subscriber is slow — skip this event.
			// Lossy by design; counted so saturation is observable via
			// DropCount. The warning is throttled to power-of-two drop
			// counts (1, 2, 4, 8, …): saturation is exactly when this
			// path runs hottest, and an unthrottled per-event log — while
			// holding the RLock — would turn a slow subscriber into a
			// log-storm that slows every publisher further.
			if n := b.dropCount.Add(1); n&(n-1) == 0 {
				slog.Warn("Pubsub buffer full; dropping events",
					"type", t, "dropped_total", n)
			}
		}
	}
}

// PublishMustDeliver delivers an event with bounded-blocking semantics.
// For each subscriber it first attempts a non-blocking send, then falls
// back to a blocking send bounded by a per-subscriber timeout (default
// [defaultMustDeliverTimeout]). On timeout the event is dropped for
// that subscriber, [Broker.MustDeliverDropCount] is incremented, and an
// error is logged. The publisher never blocks indefinitely.
//
// Use this for terminal events that must reach subscribers (finish,
// tool result, error, cancel). Callers must still tolerate rare drops
// after timeout — recovery is the subscriber's responsibility (e.g. a
// re-fetch on the next session-visible event).
func (b *Broker[T]) PublishMustDeliver(ctx context.Context, t EventType, payload T) {
	// Hold the read lock across the bounded-blocking sends below. Both
	// Shutdown and per-subscriber teardown close subscriber channels under
	// the write lock, so holding RLock here is what guarantees a send can
	// never race a close. The earlier lock-free snapshot released b.mu
	// before sending, which let Shutdown/unsubscribe close a channel
	// mid-send — a data race the race detector flags (the recover() that
	// used to sit in mustDeliver only masked the resulting panic).
	//
	// The cost is bounded and paid only under saturation: a stalled
	// subscriber can delay Subscribe/Shutdown by at most (number of
	// subscribers × mustDeliverTimeout). With this app's handful of
	// subscribers that ceiling is a few hundred milliseconds, and only
	// when a subscriber's buffer is already full.
	b.mu.RLock()
	defer b.mu.RUnlock()

	select {
	case <-b.done:
		return
	default:
	}

	event := Event[T]{Type: t, Payload: payload}
	timeout := b.mustDeliverTimeout
	for sub := range b.subs {
		if b.mustDeliver(ctx, sub, event, timeout) {
			// Context cancelled; stop delivering to the remaining subs.
			return
		}
	}
}

// mustDeliver performs one bounded-blocking send. It reports whether the
// context was cancelled (so the caller stops delivering to remaining
// subscribers). The caller holds b.mu.RLock, and every subscriber-channel
// close happens under b.mu's write lock, so the channel cannot be closed
// concurrently with this send.
func (b *Broker[T]) mustDeliver(ctx context.Context, sub chan Event[T], event Event[T], timeout time.Duration) (ctxDone bool) {
	// Fast path: non-blocking send.
	select {
	case sub <- event:
		return false
	default:
	}

	// Slow path: bounded blocking send.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case sub <- event:
	case <-timer.C:
		b.mustDeliverDropCount.Add(1)
		slog.Error("PublishMustDeliver timed out delivering event",
			"type", event.Type, "timeout", timeout)
	case <-ctx.Done():
		return true
	}
	return false
}
