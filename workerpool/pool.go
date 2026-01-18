package workerpool

import (
	"context"
	"errors"
	"sync"
)

type Job func(ctx context.Context)

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

	for {
		select {
		case <-p.ctx.Done():
			return

		case job, ok := <-p.jobs:
			if !ok {
				return
			}

			job(p.ctx)
		}
	}
}

func (p *Pool) Submit(job Job) error {
	select {
	case <-p.ctx.Done():
		return errors.New("pool is shutting down")

	case p.jobs <- job:
		return nil
	}
}

func (p *Pool) Shutdown() {
	p.once.Do(func() {
		p.cancel()
		close(p.jobs)
		p.wg.Wait()
	})
}
