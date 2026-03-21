package scheduler

import "time"

// Task represents a background refresh task to be executed.
type Task struct {
	Symbol    string
	Type      string // "analyze"
	CreatedAt time.Time
	RetryOf   int64 // ID of failed job this retries (0 if new task)
}
