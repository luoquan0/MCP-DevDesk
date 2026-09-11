from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
path = ROOT / "app" / "internal" / "agentstate" / "tasks_test.go"
text = path.read_text(encoding="utf-8")
start = text.index("func TestTaskAllowsDirtyBaseAndRejectsOnlyOverlappingChanges")
end = text.index("func initTestRepository", start)
replacement = r'''func TestTaskAllowsDirtyBaseAndRejectsOnlyOverlappingChanges(t *testing.T) {
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

'''
path.write_text(text[:start] + replacement + text[end:], encoding="utf-8", newline="\n")
print("v0.13 task test escape repair applied")
