package endgameui

import (
	"context"
	"fmt"
	"sync"
)

type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobDone    JobStatus = "done"
	JobError   JobStatus = "error"
)

// Job is a background solve (endgame solve, peg solve, or peg draw trace)
// that a client polls for completion, since these can take anywhere from
// milliseconds (cache hit) to minutes (deep multi-variation search).
type Job struct {
	ID     string
	mu     sync.Mutex
	status JobStatus
	result any
	err    string
	cancel context.CancelFunc
}

func (j *Job) Snapshot() (JobStatus, any, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status, j.result, j.err
}

func (j *Job) finish(result any, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err != nil {
		j.status = JobError
		j.err = err.Error()
		return
	}
	j.status = JobDone
	j.result = result
}

// JobManager runs and tracks background solve jobs.
type JobManager struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewJobManager() *JobManager {
	return &JobManager{jobs: make(map[string]*Job)}
}

// Start launches fn in a goroutine with a cancelable context and returns the
// tracking Job immediately.
func (jm *JobManager) Start(fn func(ctx context.Context) (any, error)) *Job {
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{ID: newID(), status: JobPending, cancel: cancel}

	jm.mu.Lock()
	jm.jobs[j.ID] = j
	jm.mu.Unlock()

	go func() {
		result, err := fn(ctx)
		j.finish(result, err)
	}()
	return j
}

func (jm *JobManager) Get(id string) (*Job, error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	j, ok := jm.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %q not found", id)
	}
	return j, nil
}

func (jm *JobManager) Cancel(id string) error {
	j, err := jm.Get(id)
	if err != nil {
		return err
	}
	j.cancel()
	return nil
}
