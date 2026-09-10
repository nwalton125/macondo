package endgameui

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJobManagerSuccess(t *testing.T) {
	jm := NewJobManager()
	j := jm.Start(func(ctx context.Context) (any, error) {
		return "hello", nil
	})
	waitForJob(t, j)
	status, result, errMsg := j.Snapshot()
	if status != JobDone {
		t.Fatalf("expected JobDone, got %v", status)
	}
	if result.(string) != "hello" {
		t.Fatalf("expected result \"hello\", got %v", result)
	}
	if errMsg != "" {
		t.Fatalf("expected empty error, got %q", errMsg)
	}
}

func TestJobManagerError(t *testing.T) {
	jm := NewJobManager()
	j := jm.Start(func(ctx context.Context) (any, error) {
		return nil, errors.New("boom")
	})
	waitForJob(t, j)
	status, _, errMsg := j.Snapshot()
	if status != JobError {
		t.Fatalf("expected JobError, got %v", status)
	}
	if errMsg != "boom" {
		t.Fatalf("expected error \"boom\", got %q", errMsg)
	}
}

func TestJobManagerCancel(t *testing.T) {
	jm := NewJobManager()
	started := make(chan struct{})
	j := jm.Start(func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started
	if err := jm.Cancel(j.ID); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	waitForJob(t, j)
	status, _, _ := j.Snapshot()
	if status != JobError {
		t.Fatalf("expected JobError after cancel, got %v", status)
	}
}

func TestJobManagerNotFound(t *testing.T) {
	jm := NewJobManager()
	if _, err := jm.Get("nonexistent"); err == nil {
		t.Fatal("expected error for unknown job id")
	}
}

func waitForJob(t *testing.T, j *Job) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if status, _, _ := j.Snapshot(); status != JobPending {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for job to finish")
}
