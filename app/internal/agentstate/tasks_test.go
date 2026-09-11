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

func TestTaskAllowsDirtyBaseAndRejectsOnlyOverlappingChanges(t *testing.T) {
	repo := initTestRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "local.txt"), []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewTaskStore(filepath.Join(t.TempDir(), "state"))
	task, err := store.Start(repo, "dirty base", "")
	if err != nil {
		t.Fatalf("dirty base should be allowed: %v", err)
	}
	if len(task.BaseDirtyFilesAtStart) != 1 || task.BaseDirtyFilesAtStart[0] != "local.txt" {
		t.Fatalf("base dirty snapshot = %#v", task.BaseDirtyFilesAtStart)
	}
	if err := os.WriteFile(filepath.Join(task.WorktreePath, "task.txt"), []byte("task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish(task.ID, "ready"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(task.ID); err != nil {
		t.Fatalf("unrelated dirty file should survive accept: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(repo, "local.txt")); err != nil || strings.ReplaceAll(string(raw), "\r\n", "\n") != "local\n" {
		t.Fatalf("local dirty file changed: %q err=%v", raw, err)
	}

	repo2 := initTestRepository(t)
	store2 := NewTaskStore(filepath.Join(t.TempDir(), "state2"))
	task2, err := store2.Start(repo2, "overlap", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task2.WorktreePath, "hello.txt"), []byte("task change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store2.Finish(task2.ID, "ready"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo2, "hello.txt"), []byte("human change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store2.Accept(task2.ID); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("expected overlap protection, got %v", err)
	}
	blocked, err := store2.Get(task2.ID)
	if err != nil || blocked.FailureReason == "" {
		t.Fatalf("failure reason was not persisted: %#v err=%v", blocked, err)
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
