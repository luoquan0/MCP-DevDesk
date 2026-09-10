package agentstate

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJobStorePersistsAndRecoversInterruptedJob(t *testing.T) {
	data := filepath.Join(t.TempDir(), "state")
	store := NewJobStore(data)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.Upsert(Job{ID: "job_1", TaskID: "tsk_1", Kind: "check:test", Command: "go", Args: []string{"test", "./..."}, CWD: ".", Running: true, StartedAt: now, UpdatedAt: now, Output: "partial"}); err != nil {
		t.Fatal(err)
	}

	reopened := NewJobStore(data)
	before, err := reopened.Get("job_1")
	if err != nil || !before.Running {
		t.Fatalf("before recovery = %#v err=%v", before, err)
	}
	if err := reopened.RecoverInterrupted(); err != nil {
		t.Fatal(err)
	}
	after, err := reopened.Get("job_1")
	if err != nil {
		t.Fatal(err)
	}
	if after.Running || after.EndedAt == "" || !strings.Contains(after.LastError, "core restarted") {
		t.Fatalf("after recovery = %#v", after)
	}
}

func TestJobStoreBoundsPersistedOutput(t *testing.T) {
	store := NewJobStore(filepath.Join(t.TempDir(), "state"))
	output := strings.Repeat("x", maxPersistedOutputBytes+100)
	if err := store.Upsert(Job{ID: "job_big", CWD: ".", Output: output, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	job, err := store.Get("job_big")
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Output) != maxPersistedOutputBytes || !job.Truncated {
		t.Fatalf("output len=%d truncated=%v", len(job.Output), job.Truncated)
	}
}
