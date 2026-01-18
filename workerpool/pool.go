package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Sentinel errors
var (
	ErrPoolClosed = errors.New("pool is shutting down")
	ErrNilExecute = errors.New("job Execute function cannot be nil")
)

// Job represents a unit of work to be executed by the pool.
type Job struct {
	Execute func(ctx context.Context) error
	Retries int           // Number of retry attempts (0 = no retries, 3 = 1 initial + 3 retries = 4 total attempts)
	Timeout time.Duration // Timeout per attempt (0 = no timeout)
}

// PoolMetrics contains statistical information about pool operations.
// Note: Failed includes all failure types. TimedOut is a subset counted separately for observability.
type PoolMetrics struct {
	Submitted uint64 // Jobs submitted to the pool
	Started   uint64 // Jobs that began execution
	Succeeded uint64 // Jobs that completed successfully
	Failed    uint64 // Jobs that failed (includes TimedOut and Panicked jobs)
	Retried   uint64 // Number of retry attempts made (not unique jobs retried)
	TimedOut  uint64 // Jobs that failed due to timeout (subset of Failed)
	Panicked  uint64 // Jobs that panicked (subset of Failed)
}

// Internal metrics storage
type metrics struct {
	submitted uint64
	started   uint64
	succeeded uint64
	failed    uint64
	retried   uint64
	timedOut  uint64
	panicked  uint64
}

// Pool manages a fixed number of worker goroutines processing jobs from a queue.
type Pool struct {
	maxWorkers int
	metrics    metrics
	jobs       chan Job

	wg     sync.WaitGroup
	quit   chan struct{} // Closed to signal shutdown
	closed int32         // Atomic flag: 0 = open, 1 = closed

	ctx    context.Context
	cancel context.CancelFunc
}

// New creates and starts a new worker pool.
// maxWorkers: number of concurrent workers (must be > 0)
// queueSize: buffered channel size for pending jobs
func New(ctx context.Context, maxWorkers, queueSize int) *Pool {
	if maxWorkers <= 0 {
		panic("maxWorkers must be > 0")
	}
	if queueSize < 0 {
		panic("queueSize cannot be negative")
	}

	ctx, cancel := context.WithCancel(ctx)

	p := &Pool{
		maxWorkers: maxWorkers,
		jobs:       make(chan Job, queueSize),
		ctx:        ctx,
		cancel:     cancel,
		quit:       make(chan struct{}),
	}

	p.start()
	return p
}

// start spawns the worker goroutines
func (p *Pool) start() {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// worker is the main goroutine loop that processes jobs
func (p *Pool) worker() {
	defer p.wg.Done()

	for {
		select {
		case job := <-p.jobs:
			p.runJob(job)

		case <-p.quit:
			// Shutdown signal received - drain remaining jobs
			p.drainJobs()
			return
		}
	}
}

// drainJobs processes all remaining jobs in the queue before worker exits
func (p *Pool) drainJobs() {
	for {
		select {
		case job := <-p.jobs:
			p.runJob(job)
		default:
			// Queue is empty
			return
		}
	}
}

// Submit enqueues a job for execution.
// Returns ErrPoolClosed if the pool has been shut down.
// Blocks if the queue is full until space is available or shutdown occurs.
func (p *Pool) Submit(job Job) error {
	if job.Execute == nil {
		return ErrNilExecute
	}

	// Fast-path check: pool already closed
	if atomic.LoadInt32(&p.closed) == 1 {
		return ErrPoolClosed
	}

	select {
	case p.jobs <- job:
		atomic.AddUint64(&p.metrics.submitted, 1)
		return nil

	case <-p.quit:
		// Shutdown happened while we were blocked
		return ErrPoolClosed

	case <-p.ctx.Done():
		// Parent context cancelled
		return fmt.Errorf("context canceled: %w", p.ctx.Err())
	}
}

// Shutdown gracefully stops the pool.
// - Prevents new job submissions
// - Allows queued jobs to complete
// - Blocks until all workers finish
// Safe to call multiple times (idempotent).
//
// Note: Jobs currently executing will complete their current attempt.
// The pool context is NOT cancelled until after workers finish,
// allowing graceful completion of in-flight work.
func (p *Pool) Shutdown() {
	// Ensure shutdown runs only once
	if !atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		return
	}

	// Signal workers to enter drain mode
	close(p.quit)

	// CRITICAL: Do NOT close p.jobs here!
	// Closing the channel would cause a panic in Submit() if there's a race
	// between the atomic check and the channel send. The channel will be
	// garbage collected when the pool is no longer referenced.

	// close(p.jobs)  // Safe now - no more Submit() calls can succeed

	// Wait for all workers to finish processing
	p.wg.Wait()

	// Cancel context after workers finish (graceful shutdown)
	p.cancel()
}

// Metrics returns a snapshot of current pool metrics.
// Thread-safe and can be called concurrently.
func (p *Pool) Metrics() PoolMetrics {
	return PoolMetrics{
		Submitted: atomic.LoadUint64(&p.metrics.submitted),
		Started:   atomic.LoadUint64(&p.metrics.started),
		Succeeded: atomic.LoadUint64(&p.metrics.succeeded),
		Failed:    atomic.LoadUint64(&p.metrics.failed),
		Retried:   atomic.LoadUint64(&p.metrics.retried),
		TimedOut:  atomic.LoadUint64(&p.metrics.timedOut),
		Panicked:  atomic.LoadUint64(&p.metrics.panicked),
	}
}

// runJob executes a job with retry logic and updates metrics
func (p *Pool) runJob(job Job) {
	atomic.AddUint64(&p.metrics.started, 1)

	var lastErr error
	maxAttempts := job.Retries + 1 // Retries=3 means 4 total attempts

RETRY_LOOP:
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Track retry attempts (not the initial attempt)
		if attempt > 0 {
			atomic.AddUint64(&p.metrics.retried, 1)
		}

		// Create per-attempt context
		attemptCtx, cancel := p.createAttemptContext(job.Timeout)

		// Execute the job with panic recovery
		err := p.executeJobSafely(attemptCtx, job.Execute)
		cancel() // Clean up context resources immediately

		lastErr = err

		// Success case
		if err == nil {
			atomic.AddUint64(&p.metrics.succeeded, 1)
			return
		}

		// Check if this was a panic (special error type)
		var panicErr *panicError
		if errors.As(err, &panicErr) {
			// Record panic and continue to failure handling
			atomic.AddUint64(&p.metrics.panicked, 1)
			// Don't retry panics unless you explicitly want to
			break RETRY_LOOP
		}

		// Check if pool is shutting down - stop retrying immediately
		select {
		case <-p.quit:
			break RETRY_LOOP
		default:
		}

		// If this was the last attempt, exit retry loop
		if attempt == maxAttempts-1 {
			break
		}

		// Exponential backoff before next retry
		backoff := time.Duration(attempt+1) * 100 * time.Millisecond
		timer := time.NewTimer(backoff)

		select {
		case <-timer.C:
			// Continue to next retry
		case <-p.quit:
			// Pool shutting down during backoff
			timer.Stop()
			break RETRY_LOOP
		case <-p.ctx.Done():
			// Parent context cancelled during backoff
			timer.Stop()
			lastErr = p.ctx.Err()
			break RETRY_LOOP
		}
	}

	// Job failed after all retries
	atomic.AddUint64(&p.metrics.failed, 1)

	// Track timeout failures separately for observability
	if errors.Is(lastErr, context.DeadlineExceeded) {
		atomic.AddUint64(&p.metrics.timedOut, 1)
	}
}

// panicError wraps a panic value as an error
type panicError struct {
	value interface{}
}

func (e *panicError) Error() string {
	return fmt.Sprintf("job panicked: %v", e.value)
}

// executeJobSafely runs the job function with panic recovery
func (p *Pool) executeJobSafely(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Job panicked and recovered: %v", r)
			err = &panicError{value: r}
		}
	}()

	return fn(ctx)
}

// createAttemptContext creates a context for a single job attempt.
// Uses p.ctx as parent, so jobs are cancelled if pool shuts down.
// For truly graceful shutdown where jobs complete regardless,
// use context.Background() as parent instead.
func (p *Pool) createAttemptContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(p.ctx, timeout)
	}
	return context.WithCancel(p.ctx)
}
