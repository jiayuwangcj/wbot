package backtestes

import (
	"context"
	"errors"
	"sync"
)

// EvaluationWorkers is deliberately fixed to the eight physical P-cores used
// by the backtest host. Keeping this independent from GOMAXPROCS makes the
// training budget and benchmark behavior explicit.
const EvaluationWorkers = 8

// EvaluationRequest is one pure parameter/window/seed evaluation.
type EvaluationRequest struct {
	Params map[string]any
	Window Window
	Seed   int64
}

type parallelResult[T any] struct {
	index int
	value T
	err   error
}

// ParallelMap evaluates all tasks with a bounded worker pool and returns
// results in task order. A task may complete in any order; no completion order
// is exposed to callers, which keeps deterministic backtest output intact.
// Errors are also selected in task order after all already-started work has
// drained, so a faster later failure cannot change the reported error.
func ParallelMap[T any](ctx context.Context, tasks []func(context.Context) (T, error)) ([]T, error) {
	return parallelMap(ctx, tasks, EvaluationWorkers)
}

func parallelMap[T any](ctx context.Context, tasks []func(context.Context) (T, error), workers int) ([]T, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	values := make([]T, len(tasks))
	if len(tasks) == 0 {
		return values, nil
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > len(tasks) {
		workers = len(tasks)
	}

	type taskResult = parallelResult[T]
	jobs := make(chan int)
	results := make(chan taskResult, len(tasks))
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}
				value, err := tasks[index](ctx)
				results <- taskResult{index: index, value: value, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range tasks {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	errs := make([]error, len(tasks))
	completed := make([]bool, len(tasks))
	for result := range results {
		values[result.index] = result.value
		errs[result.index] = result.err
		completed[result.index] = true
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range tasks {
		if !completed[i] {
			return nil, errors.New("backtestes: parallel evaluation stopped before all tasks completed")
		}
		if errs[i] != nil {
			return nil, errs[i]
		}
	}
	return values, nil
}

// ParallelEvaluate is the bounded worker-pool evaluator used by ES callers.
// The request order is retained in the returned metrics slice.
func ParallelEvaluate(ctx context.Context, eval Evaluator, requests []EvaluationRequest) ([]Metrics, error) {
	if eval == nil {
		return nil, errors.New("es: nil evaluator")
	}
	tasks := make([]func(context.Context) (Metrics, error), len(requests))
	for i := range requests {
		request := requests[i]
		tasks[i] = func(taskCtx context.Context) (Metrics, error) {
			return eval(taskCtx, request.Params, request.Window, request.Seed)
		}
	}
	return ParallelMap(ctx, tasks)
}

func evaluateWithWorkers(ctx context.Context, eval Evaluator, requests []EvaluationRequest, workers int) ([]Metrics, error) {
	if eval == nil {
		return nil, errors.New("es: nil evaluator")
	}
	tasks := make([]func(context.Context) (Metrics, error), len(requests))
	for i := range requests {
		request := requests[i]
		tasks[i] = func(taskCtx context.Context) (Metrics, error) {
			return eval(taskCtx, request.Params, request.Window, request.Seed)
		}
	}
	return parallelMap(ctx, tasks, workers)
}
