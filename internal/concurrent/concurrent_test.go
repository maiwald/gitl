package concurrent

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
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

func TestForEachVisitsEveryIndexOnce(t *testing.T) {
	seen := make([]int32, 100)
	ForEach(len(seen), 7, func(i int) { atomic.AddInt32(&seen[i], 1) })
	for i, n := range seen {
		if n != 1 {
			t.Fatalf("index %d visited %d times, want 1", i, n)
		}
	}
}

func TestForEachHonorsTheLimit(t *testing.T) {
	var inFlight, peak int32
	ForEach(50, 4, func(int) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
	})
	if peak > 4 {
		t.Errorf("peak concurrency = %d, want at most 4", peak)
	}
}

func TestForEachWithNothingToDoReturns(t *testing.T) {
	ForEach(0, 4, func(int) { t.Error("fn called with n = 0") })
}
