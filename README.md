# Concurrent Task System in Go

## About This Project

I'm new to Go, so I built this project to strengthen my understanding of concurrency. This implements a **worker pool pattern** - a practical way to handle multiple tasks concurrently without spawning unlimited goroutines.

## How the Worker Pool Works

### The Problem It Solves

Imagine you have many tasks to perform (like processing images, sending emails, or making API calls). If you do them one by one, it takes a long time. If you start too many at once, you might overwhelm your system.

A worker pool solves this by:

1. Maintaining a fixed number of "workers" (goroutines)
2. Having a queue of jobs to process
3. Workers continuously take jobs from the queue and process them

## Project Structure

```
concurrent_task_system/
├── go.mod              # Module definition
├── main.go             # Main application entry point
├── README.md           # This documentation
└── workerpool/
    └── pool.go         # Worker pool implementation
```

## Concepts I Learned & How I Used Them

### 1. **Goroutines** - Lightweight Concurrent Tasks

**What it is**: Go's way of running functions concurrently. Extremely cheap compared to OS threads.

**Why I used it**: To run multiple workers simultaneously, each processing jobs independently.

**How I used it**:

```go
go p.worker()  // Spawns a worker goroutine
```

In `start()`, I create `maxWorkers` goroutines, each running the `worker()` function.

---

### 2. **Channels** - Communication Between Goroutines

**What it is**: A typed medium for sending/receiving data between goroutines.

**Why I used it**: To safely pass jobs from the main thread to workers without data races.

**How I used it**:

```go
jobs chan Job  // Buffered channel to queue jobs
```

`Submit()` sends jobs to this channel, and workers receive them in a loop.

---

### 3. **Context** - Cancellation & Shutdown

**What it is**: A way to pass cancellation signals and deadlines across goroutines.

**Why I used it**: To gracefully shut down all workers when the pool closes, and implement per-job timeouts.

**How I used it**:

```go
ctx, cancel := context.WithCancel(ctx)
context.WithTimeout(ctx, timeout)  // Per-attempt timeout
```

Jobs can timeout independently, and pool context cancels all workers on shutdown.

---

### 4. **sync.WaitGroup** - Synchronization

**What it is**: Allows you to wait for a group of goroutines to complete.

**Why I used it**: To ensure all workers finish processing before the program exits.

**How I used it**:

```go
wg.Add(1)     // Before starting a worker
wg.Done()     // When worker completes
wg.Wait()     // In Shutdown() - blocks until all workers are done
```

---

### 5. **Atomic Operations** - Lock-free Concurrency

**What it is**: `atomic` package provides thread-safe operations on shared variables without explicit locks.

**Why I used it**: To safely update metrics counters from multiple goroutines without mutex overhead.

**How I used it**:

```go
atomic.AddUint64(&p.metrics.submitted, 1)
atomic.LoadInt32(&p.closed)
atomic.CompareAndSwapInt32(&p.closed, 0, 1)
```

Atomics are faster than mutexes for simple counters; `CompareAndSwap` implements idempotent shutdown.

---

### 6. **Select Statement** - Multiplexing

**What it is**: Waits on multiple channel operations and executes the first one that's ready.

**Why I used it**: To handle job submissions, shutdown signals, and context cancellations simultaneously.

**How I used it**:

```go
select {
case job := <-p.jobs:        // New job available
    p.runJob(job)
case <-p.quit:               // Shutdown signal
    p.drainJobs()
}
```

---

### 7. **Defer & Panic Recovery** - Fault Tolerance

**What it is**: `defer` ensures code runs even if a function panics. `recover()` catches panics.

**Why I used it**: To prevent a single job crash from killing a worker goroutine.

**How I used it**:

```go
defer func() {
    if r := recover(); r != nil {
        err = &panicError{value: r}
    }
}()
```

---

### 8. **Retry Mechanism with Exponential Backoff** - Resilience

**What it is**: Automatically retry failed jobs with increasing delays between attempts.

**Why I used it**: Network and transient failures are common; retrying improves reliability.

**How I used it**:

```go
maxAttempts := job.Retries + 1  // Retries=3 means 4 total attempts
backoff := time.Duration(attempt+1) * 100 * time.Millisecond
```

---

### 9. **Per-Job Timeout** - Deadline Enforcement

**What it is**: Set a maximum execution time for individual job attempts.

**Why I used it**: Prevent jobs from hanging indefinitely and blocking workers.

**How I used it**:

```go
type Job struct {
    Execute func(ctx context.Context) error
    Retries int
    Timeout time.Duration  // Timeout per attempt
}

attemptCtx, cancel := context.WithTimeout(p.ctx, job.Timeout)
```

Jobs that exceed their timeout fail with `context.DeadlineExceeded`.

---

### 10. **Pool Metrics & Observability** - Production Readiness

**What it is**: Collect statistics about pool operations (submitted, succeeded, failed, retried, etc.).

**Why I used it**: Monitor pool health and job success rates in production.

**How I used it**:

```go
type PoolMetrics struct {
    Submitted uint64  // Total jobs submitted
    Started   uint64  // Jobs that began execution
    Succeeded uint64  // Successful completions
    Failed    uint64  // Total failures
    Retried   uint64  // Retry attempts made
    TimedOut  uint64  // Failures due to timeout
    Panicked  uint64  // Failures due to panic
}

metrics := pool.Metrics()
```

Thread-safe snapshot accessible anytime via `pool.Metrics()`.

---

### 11. **Graceful Shutdown with Job Draining** - Clean Termination

**What it is**: Stop accepting new jobs but complete queued jobs before exiting.

**Why I used it**: Ensure no work is lost during shutdown; in-flight jobs complete normally.

**How I used it**:

```go
close(p.quit)      // Signal shutdown
p.drainJobs()      // Process remaining queue
p.wg.Wait()        // Wait for workers to finish
p.cancel()         // Cancel context after workers done
```

Job draining prevents data loss; workers exit only after queue is empty.

---

## Architecture

```
Main Thread
     |
     └─> Pool.Submit(job) ─> [jobs channel] ─> Worker 1 ─> executes job
                                    |
                                    └─> Worker 2 ─> executes job
                                    |
                                    └─> Worker 3 ─> executes job
```

The pool maintains a queue; workers pull jobs and process them concurrently.
