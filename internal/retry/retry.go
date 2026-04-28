// Package retry provides a fluent retry-with-abort loop. Forked from
// github.com/bitrise-io/go-utils/retry (v1) so the CLI no longer pulls in
// go-utils v1 just for this 80-line helper. The API and semantics match the
// upstream verbatim — call sites can switch back via a single import path
// change if/when go-utils/v2 grows an equivalent.
package retry

import (
	"fmt"
	"time"
)

// Action runs once per attempt. Returns nil to stop the loop, an error to retry.
type Action func(attempt uint) error

// AbortableAction is like Action but the second return value short-circuits
// the loop on a non-retryable error (true → stop immediately).
type AbortableAction func(attempt uint) (error, bool)

// Model carries the retry configuration. Construct via Times / Wait or chain
// from an existing Model.
type Model struct {
	retry    uint
	waitTime time.Duration
}

// Times creates a new Model that retries up to `retry` additional times after
// the first attempt.
func Times(retry uint) *Model {
	m := Model{}

	return m.Times(retry)
}

// Times sets the retry count on an existing Model and returns the receiver
// for chaining.
func (m *Model) Times(retry uint) *Model {
	m.retry = retry

	return m
}

// Wait creates a new Model with the inter-attempt wait set.
func Wait(waitTime time.Duration) *Model {
	m := Model{}

	return m.Wait(waitTime)
}

// Wait sets the inter-attempt wait on an existing Model and returns the
// receiver for chaining.
func (m *Model) Wait(waitTime time.Duration) *Model {
	m.waitTime = waitTime

	return m
}

// Try continues executing action while it returns an error, up to the
// configured retry count. Returns the last error.
func (m *Model) Try(action Action) error {
	return m.TryWithAbort(func(attempt uint) (error, bool) {
		return action(attempt), false
	})
}

// TryWithAbort runs action up to retry+1 times, sleeping waitTime between
// attempts. Returning (nil, _) stops the loop with success; returning
// (err, true) stops the loop with that error (non-retryable). Otherwise the
// loop continues until the retry count is exhausted.
func (m *Model) TryWithAbort(action AbortableAction) error {
	if action == nil {
		return fmt.Errorf("no action specified")
	}

	var err error
	var shouldAbort bool

	for attempt := uint(0); (attempt == 0 || err != nil) && attempt <= m.retry; attempt++ {
		if attempt > 0 && m.waitTime > 0 {
			time.Sleep(m.waitTime)
		}

		err, shouldAbort = action(attempt)

		if shouldAbort {
			break
		}
	}

	return err
}
