package mcpcore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mcp-devdesk/internal/agentstate"
)

func TestTaskToolsRedirectWritesIntoIsolatedWorktreeAndRequireHumanAccept(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "MCP Test"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "base.txt"}, {"commit", "-m", "base"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	data := filepath.Join(t.TempDir(), "data")
	server := mustNewServer(t, Options{Workspace: repo, AgentStateDir: data, PermissionMode: "trusted", ToolProfile: "full"})
	started, err := server.executeTool("task_start", map[string]any{"title": "isolated edit"})
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := started["taskId"].(string)
	if taskID == "" {
		t.Fatalf("task start result = %#v", started)
	}
	if _, err := server.executeTool("write_file", map[string]any{"path": "task.txt", "content": "inside task\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "task.txt")); !os.IsNotExist(err) {
		t.Fatalf("task write escaped into base workspace: %v", err)
	}
	if _, err := server.executeTool("task_finish", map[string]any{"taskId": taskID, "summary": "ready"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.executeTool("write_file", map[string]any{"path": "blocked.txt", "content": "blocked"}); err == nil || !strings.Contains(err.Error(), "awaiting human review") {
		t.Fatalf("review task allowed mutation: %v", err)
	}
	if _, err := server.executeTool("task_accept", map[string]any{"taskId": taskID}); err == nil {
		t.Fatal("MCP client unexpectedly has a task_accept capability")
	}

	store := agentstate.NewTaskStore(data)
	if _, err := store.Accept(taskID); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(repo, "task.txt")); err != nil || string(raw) != "inside task\n" {
		t.Fatalf("accepted task content = %q err=%v", raw, err)
	}
}
