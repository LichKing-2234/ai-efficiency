package readcache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlightGroupCollapsesConcurrentLoads(t *testing.T) {
	var group FlightGroup[int]
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) (int, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return 42, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}

	const callers = 50
	results := make(chan int, callers)
	errorsCh := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		go func() {
			ready.Done()
			<-start
			value, err := group.Do(context.Background(), "same-key", time.Second, loader)
			results <- value
			errorsCh <- err
		}()
	}
	ready.Wait()
	close(start)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("loader did not start")
	}
	deadline := time.Now().Add(time.Second)
	for {
		group.mu.Lock()
		waiters := 0
		if call := group.calls["same-key"]; call != nil {
			waiters = call.waiters
		}
		group.mu.Unlock()
		if waiters == callers {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("callers did not join one flight; waiter count = %d", waiters)
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	for i := 0; i < callers; i++ {
		if err := <-errorsCh; err != nil {
			t.Fatalf("caller error = %v", err)
		}
		if value := <-results; value != 42 {
			t.Fatalf("caller value = %d, want 42", value)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want 1", got)
	}
}

func TestFlightGroupCancelsSharedLoadWhenFinalWaiterLeaves(t *testing.T) {
	var group FlightGroup[int]
	loaderStarted := make(chan struct{})
	loaderStopped := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := group.Do(ctx, "cancel-key", time.Minute, func(shared context.Context) (int, error) {
			close(loaderStarted)
			<-shared.Done()
			loaderStopped <- shared.Err()
			return 0, shared.Err()
		})
		done <- err
	}()
	select {
	case <-loaderStarted:
	case <-time.After(time.Second):
		t.Fatal("loader did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter error = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not return")
	}
	select {
	case err := <-loaderStopped:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("loader error = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shared loader was left running")
	}
}

func TestFlightGroupKeepsLoadForRemainingWaiter(t *testing.T) {
	var group FlightGroup[int]
	loaderStarted := make(chan struct{})
	release := make(chan struct{})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	secondValue := make(chan int, 1)

	loader := func(shared context.Context) (int, error) {
		select {
		case <-loaderStarted:
		default:
			close(loaderStarted)
		}
		select {
		case <-release:
			return 77, nil
		case <-shared.Done():
			return 0, shared.Err()
		}
	}
	go func() {
		_, err := group.Do(firstCtx, "shared-key", time.Second, loader)
		firstDone <- err
	}()
	<-loaderStarted
	go func() {
		value, err := group.Do(context.Background(), "shared-key", time.Second, loader)
		secondValue <- value
		secondDone <- err
	}()

	deadline := time.Now().Add(time.Second)
	for {
		group.mu.Lock()
		waiters := 0
		if call := group.calls["shared-key"]; call != nil {
			waiters = call.waiters
		}
		group.mu.Unlock()
		if waiters == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second waiter did not join; waiter count = %d", waiters)
		}
		time.Sleep(time.Millisecond)
	}
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first waiter error = %v, want canceled", err)
	}
	close(release)
	if err := <-secondDone; err != nil {
		t.Fatalf("second waiter error = %v", err)
	}
	if value := <-secondValue; value != 77 {
		t.Fatalf("second waiter value = %d, want 77", value)
	}
}
