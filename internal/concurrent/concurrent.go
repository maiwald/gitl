// Package concurrent holds small, fully generic concurrency helpers with
// no dependency on git or the TUI, so they can be shared by internal/git
// and internal/ui without either package depending on the other for them.
package concurrent

import "sync"

// RunConcurrently runs each fn in its own goroutine and waits for all of them
// to finish, returning the first non-nil error in call order, not completion
// order. Used to overlap independent operations — several git subprocess
// calls, or two unrelated repo queries — that have nothing to serialize them.
func RunConcurrently(fns ...func() error) error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	wg.Add(len(fns))
	for i, fn := range fns {
		go func(i int, fn func() error) {
			defer wg.Done()
			errs[i] = fn()
		}(i, fn)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// ForEach calls fn(i) for every i in [0, n), with at most limit calls in
// flight at once, and returns when all have finished. Each call is expected to
// write only to its own slot of whatever result it fills, which is what makes
// no further synchronization necessary. For the same independent-work shape as
// RunConcurrently, when there are too many items to start them all at once.
func ForEach(n, limit int, fn func(i int)) {
	sem := make(chan struct{}, max(limit, 1))
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}()
	}
	wg.Wait()
}
