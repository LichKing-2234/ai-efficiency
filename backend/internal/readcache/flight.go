package readcache

import (
	"context"
	"sync"
	"time"
)

type FlightGroup[T any] struct {
	mu    sync.Mutex
	calls map[string]*flightCall[T]
}

type flightCall[T any] struct {
	done      chan struct{}
	cancel    context.CancelFunc
	waiters   int
	completed bool
	value     T
	err       error
}

func (g *FlightGroup[T]) Do(ctx context.Context, key string, timeout time.Duration, load func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*flightCall[T])
	}
	if call := g.calls[key]; call != nil {
		call.waiters++
		g.mu.Unlock()
		return g.wait(ctx, key, call)
	}

	sharedCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	call := &flightCall[T]{done: make(chan struct{}), cancel: cancel, waiters: 1}
	g.calls[key] = call
	g.mu.Unlock()

	go func() {
		value, err := load(sharedCtx)
		cancel()
		g.mu.Lock()
		call.value = value
		call.err = err
		call.completed = true
		if g.calls[key] == call {
			delete(g.calls, key)
		}
		close(call.done)
		g.mu.Unlock()
	}()

	return g.wait(ctx, key, call)
}

func (g *FlightGroup[T]) wait(ctx context.Context, key string, call *flightCall[T]) (T, error) {
	var zero T
	select {
	case <-call.done:
		return call.value, call.err
	case <-ctx.Done():
		select {
		case <-call.done:
			return call.value, call.err
		default:
		}
		g.leave(key, call)
		return zero, ctx.Err()
	}
}

func (g *FlightGroup[T]) leave(key string, call *flightCall[T]) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if call.completed {
		return
	}
	call.waiters--
	if call.waiters > 0 {
		return
	}
	if g.calls[key] == call {
		delete(g.calls, key)
	}
	call.cancel()
}
