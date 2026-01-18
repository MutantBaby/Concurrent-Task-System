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

	pool := workerpool.New(ctx, 3, 2)
	fmt.Println("Worker pool started")

	for i := 1; i <= 10; i++ {
		jobID := i

		err := pool.Submit(func(ctx context.Context) {
			fmt.Printf("Job %d started\n", jobID)

			select {
			case <-time.After(2 * time.Second):
				fmt.Printf("Job %d completed\n", jobID)
			case <-ctx.Done():
				fmt.Printf("Job %d cancelled\n", jobID)
			}
		})

		if err != nil {
			log.Printf("Failed to submit job %d: %v\n", jobID, err)
		}
	}

	time.Sleep(5 * time.Second)

	fmt.Println("Shutting down pool...")
	pool.Shutdown()

	fmt.Println("Pool shutdown complete")
}
