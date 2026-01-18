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

**Why I used it**: To gracefully shut down all workers when the pool closes.

**How I used it**:

```go
ctx, cancel := context.WithCancel(ctx)
```

When `Shutdown()` is called, `cancel()` signals all workers to stop.

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

### 5. **sync.Once** - Single Execution

**What it is**: Ensures a function runs exactly once, even if called multiple times.

**Why I used it**: To prevent multiple shutdown attempts that could cause errors.

**How I used it**:

```go
once.Do(func() {
    // Shutdown logic runs only once
})
```

---

### 6. **Select Statement** - Multiplexing

**What it is**: Waits on multiple channel operations and executes the first one that's ready.

**Why I used it**: To make workers respond to either new jobs or shutdown signals, and handle timeouts during retries.

**How I used it**:

```go
select {
case <-p.ctx.Done():    // Shutdown signal
    return
case job, ok := <-p.jobs:  // New job
    job(p.ctx)
}
```

---

### 7. **Defer & Panic Recovery** - Fault Tolerance

**What it is**: `defer` ensures code runs even if a function panics. `recover()` catches panics.

**Why I used it**: To prevent a single job crash from killing an entire worker goroutine.

**How I used it**:

```go
defer func() {
    if r := recover(); r != nil {
        log.Println("worker recovered from panic:", r)
    }
}()
```

Now if a job panics, the worker logs the error and continues processing other jobs instead of crashing.

---

### 8. **Retry Mechanism with Exponential Backoff** - Resilience

**What it is**: Automatically retry failed jobs with increasing delays between attempts.

**Why I used it**: Network and transient failures are common; retrying improves reliability.

**How I used it**:

```go
type Job struct {
    Execute func(ctx context.Context) error
    Retries int
    Timeout time.Duration
}

// In runJob():
for attempt := 0; attempt <= job.Retries; attempt++ {
    err = job.Execute(ctx)

    if err == nil {
        return
    }

    backoff := time.Duration(attempt+1) * 100 * time.Millisecond
    // wait before retrying
}
```

Job structure now includes:

- `Execute`: The actual work function that returns an error
- `Retries`: Number of retry attempts if job fails
- `Timeout`: Optional timeout for the job

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
