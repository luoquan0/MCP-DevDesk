package projecttools

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if !details.Git || details.ChangedFiles < 1 || !details.HasAgents || len(details.Skills) != 1 {
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

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", command...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
