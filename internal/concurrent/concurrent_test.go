package concurrent

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestRunConcurrentlySucceedsWhenAllSucceed(t *testing.T) {
	var calls int32
	err := RunConcurrently(
		func() error { atomic.AddInt32(&calls, 1); return nil },
		func() error { atomic.AddInt32(&calls, 1); return nil },
		func() error { atomic.AddInt32(&calls, 1); return nil },
	)
	if err != nil {
		t.Fatalf("RunConcurrently: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (every fn actually ran)", calls)
	}
}

func TestRunConcurrentlyReturnsFirstErrorInCallOrder(t *testing.T) {
	errA := errors.New("a")
	errB := errors.New("b")

	// Both fail; the returned error must be errA (call order), regardless
	// of which goroutine happens to finish first.
	err := RunConcurrently(
		func() error { return errA },
		func() error { return errB },
	)
	if err != errA {
		t.Fatalf("RunConcurrently = %v, want %v (first in call order)", err, errA)
	}
}

func TestRunConcurrentlyPropagatesASingleError(t *testing.T) {
	want := errors.New("boom")
	err := RunConcurrently(
		func() error { return nil },
		func() error { return want },
		func() error { return nil },
	)
	if err != want {
		t.Fatalf("RunConcurrently = %v, want %v", err, want)
	}
}
