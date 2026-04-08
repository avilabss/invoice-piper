package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	emailMailboxesWorkflowTimeout = 5 * time.Minute
	emailExportWorkflowTimeout    = 20 * time.Minute
	workflowTimeoutDrainGrace     = 100 * time.Millisecond
)

func runWithWorkflowTimeout[T any](ctx context.Context, run func() (T, error)) (T, error) {
	type runResult struct {
		value T
		err   error
	}

	resultCh := make(chan runResult, 1)
	go func() {
		value, err := run()
		resultCh <- runResult{value: value, err: err}
	}()

	select {
	case result := <-resultCh:
		return result.value, result.err
	case <-ctx.Done():
		select {
		case result := <-resultCh:
			return result.value, result.err
		case <-time.After(workflowTimeoutDrainGrace):
			var zero T
			return zero, ctx.Err()
		}
	}
}

func workflowTimedOut(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

func workflowTimeoutError(workflow string, timeout time.Duration, err error) error {
	cause := context.DeadlineExceeded
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		cause = errors.Join(cause, err)
	}

	return fmt.Errorf("%s timed out after %s: %w", workflow, timeout, cause)
}
