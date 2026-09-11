package agentstate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskLifecycleAcceptsIsolatedWorktree(t *testing.T) {
	repo := initTestRepository(t)
	store := NewTaskStore(filepath.Join(t.TempDir(), "state"))

	task, err := store.Start(repo, "change greeting", "isolated change")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == "" || task.Status != "editing" || task.WorktreePath == repo {
		t.Fatalf("unexpected task: %#v", task)
	}
	if err := os.WriteFile(filepath.Join(task.WorktreePath, "hello.txt"), []byte("updated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	baseRaw, err := os.ReadFile(filepath.Join(repo, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(baseRaw) != "base\n" {
		t.Fatalf("base changed before accept: %q", baseRaw)
	}

	finished, err := store.Finish(task.ID, "ready")
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "review" {
		t.Fatalf("finish status = %q", finished.Status)
	}

	accepted, err := store.Accept(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != "accepted" || accepted.ResultCommit == "" {
		t.Fatalf("accepted task = %#v", accepted)
	}
	raw, err := os.ReadFile(filepath.Join(repo, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(raw), "\r\n", "\n") != "updated\n" {
		t.Fatalf("accepted content = %q", raw)
	}
	if _, err := os.Stat(task.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after accept: %v", err)
	}
	if _, active, err := store.Active(); err != nil || active {
		t.Fatalf("active after accept = %v, err=%v", active, err)
	}
}

func TestTaskRejectPreservesBaseAndPersists(t *testing.T) {
	repo := initTestRepository(t)
	data := filepath.Join(t.TempDir(), "state")
	store := NewTaskStore(data)
	task, err := store.Start(repo, "discard me", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task.WorktreePath, "hello.txt"), []byte("discarded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish(task.ID, "review"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reject(task.ID); err != nil {
		t.Fatal(err)
	}

	reopened := NewTaskStore(data)
	persisted, err := reopened.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != "rejected" {
		t.Fatalf("persisted status = %q", persisted.Status)
	}
	raw, err := os.ReadFile(filepath.Join(repo, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "base\n" {
		t.Fatalf("base changed after reject: %q", raw)
	}
}

func TestTaskStartRequiresCleanBase(t *testing.T) {
	repo := initTestRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewTaskStore(filepath.Join(t.TempDir(), "state"))
	if _, err := store.Start(repo, "blocked", ""); err == nil || !strings.Contains(err.Error(), "clean base") {
		t.Fatalf("expected clean base error, got %v", err)
	}
}

func initTestRepository(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, repo, "init")
	gitTestRun(t, repo, "config", "user.email", "test@example.com")
	gitTestRun(t, repo, "config", "user.name", "MCP DevDesk Test")
	if err := os.WriteFile(filepath.Join(repo, "hello.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, repo, "add", "hello.txt")
	gitTestRun(t, repo, "commit", "-m", "base")
	return repo
}

func gitTestRun(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
