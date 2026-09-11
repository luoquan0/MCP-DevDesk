package agentstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	jobStoreVersion         = 1
	maxPersistedJobs        = 200
	maxPersistedOutputBytes = 512 * 1024
)

type Job struct {
	ID        string   `json:"id"`
	TaskID    string   `json:"taskId,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	CWD       string   `json:"cwd"`
	Running   bool     `json:"running"`
	ExitCode  *int     `json:"exitCode,omitempty"`
	LastError string   `json:"lastError,omitempty"`
	Output    string   `json:"output,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	StartedAt string   `json:"startedAt"`
	EndedAt   string   `json:"endedAt,omitempty"`
	UpdatedAt string   `json:"updatedAt"`
}

type jobFile struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}
type JobStore struct{ path string }

func NewJobStore(dataDir string) *JobStore {
	return &JobStore{path: filepath.Join(dataDir, "agent-runtime", "jobs.json")}
}

func (s *JobStore) RecoverInterrupted() error {
	return s.mutate(func(state *jobFile) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for i := range state.Jobs {
			if !state.Jobs[i].Running {
				continue
			}
			state.Jobs[i].Running = false
			if state.Jobs[i].LastError == "" {
				state.Jobs[i].LastError = "job interrupted because the MCP core restarted"
			}
			state.Jobs[i].EndedAt = now
			state.Jobs[i].UpdatedAt = now
		}
		return nil
	})
}

func (s *JobStore) Upsert(job Job) error {
	if strings.TrimSpace(job.ID) == "" {
		return errors.New("job id is required")
	}
	if len(job.Output) > maxPersistedOutputBytes {
		job.Output = job.Output[len(job.Output)-maxPersistedOutputBytes:]
		job.Truncated = true
	}
	return s.mutate(func(state *jobFile) error {
		for i := range state.Jobs {
			if state.Jobs[i].ID == job.ID {
				state.Jobs[i] = job
				return nil
			}
		}
		state.Jobs = append(state.Jobs, job)
		if len(state.Jobs) > maxPersistedJobs {
			sort.SliceStable(state.Jobs, func(i, j int) bool { return state.Jobs[i].UpdatedAt > state.Jobs[j].UpdatedAt })
			state.Jobs = state.Jobs[:maxPersistedJobs]
		}
		return nil
	})
}

func (s *JobStore) Get(id string) (Job, error) {
	state, err := s.load()
	if err != nil {
		return Job{}, err
	}
	for _, job := range state.Jobs {
		if job.ID == strings.TrimSpace(id) {
			return job, nil
		}
	}
	return Job{}, errors.New("job not found")
}

func (s *JobStore) List(limit int, taskID string) ([]Job, error) {
	state, err := s.load()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	taskID = strings.TrimSpace(taskID)
	result := make([]Job, 0, len(state.Jobs))
	for _, job := range state.Jobs {
		if taskID == "" || job.TaskID == taskID {
			result = append(result, job)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].UpdatedAt > result[j].UpdatedAt })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *JobStore) load() (jobFile, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return jobFile{Version: jobStoreVersion, Jobs: []Job{}}, nil
	}
	if err != nil {
		return jobFile{}, fmt.Errorf("read agent job state: %w", err)
	}
	var state jobFile
	if err := json.Unmarshal(raw, &state); err != nil {
		return jobFile{}, fmt.Errorf("parse agent job state: %w", err)
	}
	if state.Version != jobStoreVersion {
		return jobFile{}, fmt.Errorf("unsupported agent job state version %d", state.Version)
	}
	if state.Jobs == nil {
		state.Jobs = []Job{}
	}
	return state, nil
}

func (s *JobStore) mutate(update func(*jobFile) error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	lockPath := s.path + ".lock"
	deadline := time.Now().Add(3 * time.Second)
	var lock *os.File
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			lock = file
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("agent job state is busy; retry shortly")
		}
		time.Sleep(40 * time.Millisecond)
	}
	_ = lock.Close()
	defer os.Remove(lockPath)
	state, err := s.load()
	if err != nil {
		return err
	}
	if err := update(&state); err != nil {
		return err
	}
	state.Version = jobStoreVersion
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".jobs-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := func() { _ = temp.Close(); _ = os.Remove(tempPath) }
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(append(raw, '\n')); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
