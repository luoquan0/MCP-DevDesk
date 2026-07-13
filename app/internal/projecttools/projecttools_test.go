package projecttools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectAndDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills", "demo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agents", "skills", "demo", "SKILL.md"), []byte("demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	details, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !details.Git || details.CurrentCommit == "" || details.CurrentShort == "" || details.ChangedFiles < 1 || !details.HasAgents || len(details.Skills) != 1 {
		t.Fatalf("unexpected details: %+v", details)
	}
	diff, err := GetDiff(root)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Text == "" {
		t.Fatal("expected diff")
	}
}

func TestHistoryAndSafeRollback(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "History Tester")
	file := filepath.Join(root, "version.txt")
	commits := make([]string, 0, 3)
	for _, value := range []string{"one\n", "two\n", "three\n"} {
		if err := os.WriteFile(file, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", "version.txt")
		runGit(t, root, "commit", "-m", "version "+strings.TrimSpace(value))
		commits = append(commits, strings.TrimSpace(runGitOutput(t, root, "rev-parse", "HEAD")))
	}

	history, err := GetHistory(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !history.Truncated || len(history.Commits) != 2 {
		t.Fatalf("unexpected history size: %+v", history)
	}
	if history.CurrentCommit != commits[2] || !history.Commits[0].Current || history.Commits[0].Subject != "version three" {
		t.Fatalf("unexpected current history record: %+v", history)
	}
	if history.Commits[0].Hash == "" || history.Commits[0].ShortHash == "" || history.Commits[0].Author != "History Tester" {
		t.Fatalf("history metadata incomplete: %+v", history.Commits[0])
	}

	rollback, err := Rollback(root, commits[0])
	if err != nil {
		t.Fatal(err)
	}
	if rollback.PreviousCommit != commits[2] || rollback.CurrentCommit != commits[0] || rollback.BackupBranch == "" {
		t.Fatalf("unexpected rollback result: %+v", rollback)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != "one" {
		t.Fatalf("rollback content = %q", content)
	}
	if strings.TrimSpace(runGitOutput(t, root, "rev-parse", rollback.BackupBranch)) != commits[2] {
		t.Fatal("backup branch does not preserve the previous HEAD")
	}

	if err := os.WriteFile(file, []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Rollback(root, commits[0]); err == nil || !strings.Contains(err.Error(), "未提交修改") {
		t.Fatalf("dirty rollback error = %v", err)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", command...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func runGitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", command...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}
