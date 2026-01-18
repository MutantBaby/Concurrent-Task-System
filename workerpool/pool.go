package workerpool

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

type Job struct {
	Execute func(ctx context.Context) error
	Retries int
	Timeout time.Duration // optional
}

type Pool struct {
	maxWorkers int
	jobs       chan Job
	once       sync.Once
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

func New(ctx context.Context, maxWorkers, queueSize int) *Pool {
	if maxWorkers <= 0 {
		panic("maxWorkers must be > 0")
	}

	ctx, cancel := context.WithCancel(ctx)

	p := &Pool{
		maxWorkers: maxWorkers,
		jobs:       make(chan Job, queueSize),
		ctx:        ctx,
		cancel:     cancel,
	}

	p.start()
	return p
}

func (p *Pool) start() {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)

		go p.worker()
	}
}

func (p *Pool) worker() {
	defer p.wg.Done()

	// panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.Println("worker recovered from panic:", r)
		}
	}()

	for {
		select {
		case <-p.ctx.Done():
			return

		case job, ok := <-p.jobs: // receiving job from channel
			if !ok {
				return
			}

			p.runJob(job)
		}
	}
}

func (p *Pool) Submit(job Job) error {
	select {
	case <-p.ctx.Done():
		return errors.New("pool is shutting down")

	case p.jobs <- job: // adding job to channel
		return nil
	}
}

func (p *Pool) Shutdown() {
	p.once.Do(func() {
		p.cancel()    // signaling for lifecycle closing
		close(p.jobs) // closing channels
		p.wg.Wait()   // waiting for workers to complete lifecycle.
	})
}

func (p *Pool) runJob(job Job) {
	ctx := p.ctx
	var err error

	for attempt := 0; attempt <= job.Retries; attempt++ {
		err = job.Execute(ctx) // executing actual job

		if err == nil {
			return
		}

		// retry waits longer with each attempt -> 100ms, 200ms, 300ms...
		backoff := time.Duration(attempt+1) * 100 * time.Millisecond

		select {
		// the receive <-time.After(...) blocks the goroutine until the timer fires
		case <-time.After(backoff): // timer expired → continue retry
		case <-ctx.Done():
			return
		}
	}
}
