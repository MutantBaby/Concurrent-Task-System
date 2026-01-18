package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"concurrent_task_system/workerpool"
)

func main() {
	ctx := context.Background()

	pool := workerpool.New(ctx, 3, 2) // initiating a pool
	fmt.Println("Worker pool started")

	for i := 1; i <= 10; i++ {
		jobID := i

		// submitting jobs to workers
		err := pool.Submit(workerpool.Job{
			Retries: 3, // add retries
			Timeout: 2 * time.Second,
			Execute: func(ctx context.Context) error {
				fmt.Printf("Job %d started\n", jobID)

				select {
				case <-time.After(2 * time.Second):
					fmt.Printf("Job %d completed\n", jobID)
				case <-ctx.Done():
					fmt.Printf("Job %d timed out or cancelled\n", jobID)
					return ctx.Err()
				}

				return nil
			},
		})

		if err != nil {
			log.Printf("Failed to submit job %d: %v\n", jobID, err)
		}
	}

	time.Sleep(5 * time.Second) // doing some work

	fmt.Println("Shutting down pool...")
	pool.Shutdown()

	fmt.Println("Pool shutdown complete")

	metrics := pool.Metrics()

	fmt.Println("\n---- Metrics ----")
	fmt.Println("Submitted:", metrics.Submitted)
	fmt.Println("Started:", metrics.Started)
	fmt.Println("Succeeded:", metrics.Succeeded)
	fmt.Println("Failed:", metrics.Failed)
	fmt.Println("Retried:", metrics.Retried)
	fmt.Println("Timed out:", metrics.TimedOut)
}
