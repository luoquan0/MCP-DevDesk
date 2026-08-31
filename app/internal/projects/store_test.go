package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorePersistsAndProtectsActiveProject(t *testing.T) {
	dataDir := t.TempDir()
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	if err := os.MkdirAll(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(second, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(dataDir, first)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.Add("Second", second)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(store.List()))
	}
	if err := store.Remove(project.ID, first); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(dataDir, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 1 {
		t.Fatalf("expected persisted project, got %d", len(reloaded.List()))
	}
	active := reloaded.List()[0]
	if err := reloaded.Remove(active.ID, first); err == nil {
		t.Fatal("expected active project removal to fail")
	}
}

func TestStoreNormalizesAndDeduplicatesPersistedProjects(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	older := Project{ID: "old-a", Name: "Project", Path: projectDir, AddedAt: time.Unix(10, 0), LastOpenedAt: time.Unix(20, 0)}
	newer := Project{ID: "old-b", Name: "Duplicate", Path: projectDir + string(filepath.Separator), AddedAt: time.Unix(15, 0), LastOpenedAt: time.Unix(30, 0)}
	raw, err := json.Marshal([]Project{older, newer})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "projects.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(dataDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	items := store.List()
	if len(items) != 1 {
		t.Fatalf("expected duplicate paths to be merged, got %d", len(items))
	}
	if items[0].ID != projectID(projectDir) {
		t.Fatalf("expected regenerated stable ID, got %q", items[0].ID)
	}
	if !items[0].LastOpenedAt.Equal(newer.LastOpenedAt) {
		t.Fatalf("expected newest last-opened timestamp, got %v", items[0].LastOpenedAt)
	}
}

func TestStoreUpdatesProjectPathAndRejectsDuplicates(t *testing.T) {
	dataDir := t.TempDir()
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	third := filepath.Join(t.TempDir(), "third")
	for _, path := range []string{first, second, third} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	store, err := NewStore(dataDir, first)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.Add("Second project", second)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdatePath(project.ID, third)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Path != filepath.Clean(third) {
		t.Fatalf("updated path = %q, want %q", updated.Path, filepath.Clean(third))
	}
	if updated.ID == project.ID {
		t.Fatal("expected project ID to change with the path")
	}
	if updated.Name != project.Name {
		t.Fatalf("project name changed from %q to %q", project.Name, updated.Name)
	}
	if _, ok := store.Get(updated.ID); !ok {
		t.Fatal("updated project was not persisted under its new ID")
	}
	if _, err := store.UpdatePath(updated.ID, first); err == nil {
		t.Fatal("expected duplicate project path to be rejected")
	}
}

func TestProjectFoldersPersistAndAssignWithoutMovingProject(t *testing.T) {
	dataDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "folder-project")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(dataDir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := store.AddFolder("客户项目/2026")
	if err != nil {
		t.Fatal(err)
	}
	project := store.List()[0]
	updated, err := store.SetFolder(project.ID, folder)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Folder != "客户项目/2026" {
		t.Fatalf("project folder = %q", updated.Folder)
	}
	if updated.Path != filepath.Clean(workspace) {
		t.Fatalf("project path changed while assigning virtual folder: %q", updated.Path)
	}

	reloaded, err := NewStore(dataDir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Folders()) != 1 || reloaded.Folders()[0] != "客户项目/2026" {
		t.Fatalf("folders were not persisted: %#v", reloaded.Folders())
	}
	reloadedProject, ok := reloaded.Get(project.ID)
	if !ok || reloadedProject.Folder != "客户项目/2026" {
		t.Fatalf("project folder assignment was not persisted: %#v", reloadedProject)
	}
}

func TestStoreRollsBackFailedTouchAndRemove(t *testing.T) {
	dataDir := t.TempDir()
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewStore(dataDir, first)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.Add("Second", second)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := store.Get(project.ID)
	store.path = dataDir
	if err := store.Touch(project.ID); err == nil {
		t.Fatal("expected touch save to fail")
	}
	afterTouch, _ := store.Get(project.ID)
	if !afterTouch.LastOpenedAt.Equal(before.LastOpenedAt) {
		t.Fatal("touch timestamp changed after failed save")
	}
	if err := store.Remove(project.ID, first); err == nil {
		t.Fatal("expected remove save to fail")
	}
	if _, ok := store.Get(project.ID); !ok {
		t.Fatal("project was removed from memory after failed save")
	}
}

func TestProjectPromptUsesAgentsFileAndGlobalPromptIsSeparate(t *testing.T) {
	dataDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "prompt-project")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(dataDir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	project := store.List()[0]
	if err := store.SetPromptSettings(true, "完成全部步骤后再回复用户"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdatePrompt(project.ID, "本项目修改代码后必须运行测试")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Prompt == "" {
		t.Fatal("project prompt was not stored")
	}
	rawAgents, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawAgents), "本项目修改代码后必须运行测试") {
		t.Fatalf("AGENTS.md did not contain the project prompt: %q", rawAgents)
	}
	effective := store.EffectivePrompt(workspace)
	if !strings.Contains(effective, "完成全部步骤后再回复用户") || strings.Contains(effective, "本项目修改代码后必须运行测试") {
		t.Fatalf("managed global prompt must remain separate from AGENTS.md: %q", effective)
	}

	reloaded, err := NewStore(dataDir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GlobalPrompt() != "完成全部步骤后再回复用户" {
		t.Fatalf("global prompt was not persisted: %q", reloaded.GlobalPrompt())
	}
	if !reloaded.GlobalPromptEnabled() {
		t.Fatal("global prompt enabled state was not persisted")
	}
	reloadedProject, ok := reloaded.Get(project.ID)
	if !ok || reloadedProject.Prompt != "本项目修改代码后必须运行测试" {
		t.Fatalf("project AGENTS.md was not reloaded: %#v", reloadedProject)
	}
	projectsRaw, err := os.ReadFile(filepath.Join(dataDir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(projectsRaw), "本项目修改代码后必须运行测试") {
		t.Fatalf("project prompt must not be persisted in DevDesk projects.json: %s", projectsRaw)
	}
}

func TestGlobalPromptSwitchControlsInjection(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPromptSettings(false, "保留但不要注入"); err != nil {
		t.Fatal(err)
	}
	if got := store.EffectivePrompt(""); got != "" {
		t.Fatalf("disabled global prompt must not be injected: %q", got)
	}
	if err := store.SetPromptSettings(true, "保留但不要注入"); err != nil {
		t.Fatal(err)
	}
	if got := store.EffectivePrompt(""); !strings.Contains(got, "保留但不要注入") {
		t.Fatalf("enabled global prompt was not injected: %q", got)
	}
}

func TestLegacyStoredPromptsMigrateToAgentsAndExplicitGlobalSwitch(t *testing.T) {
	dataDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "legacy-project")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	legacyProjects := []Project{{
		ID:           "legacy-id",
		Name:         "Legacy",
		Path:         workspace,
		Prompt:       "旧项目规则必须迁移到项目目录",
		AddedAt:      now,
		LastOpenedAt: now,
	}}
	raw, err := json.MarshalIndent(legacyProjects, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "projects.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "project-prompts.json"), []byte("{\"globalPrompt\":\"旧全局规则\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(dataDir, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !store.GlobalPromptEnabled() || store.GlobalPrompt() != "旧全局规则" {
		t.Fatalf("legacy global prompt was not migrated with enabled switch: enabled=%v prompt=%q", store.GlobalPromptEnabled(), store.GlobalPrompt())
	}
	agentsRaw, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agentsRaw), "旧项目规则必须迁移到项目目录") {
		t.Fatalf("legacy project prompt was not migrated to AGENTS.md: %q", agentsRaw)
	}
	persisted, err := os.ReadFile(filepath.Join(dataDir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted), "旧项目规则必须迁移到项目目录") {
		t.Fatalf("legacy prompt still exists in projects.json: %s", persisted)
	}
}

func TestProjectPromptSizeLimit(t *testing.T) {
	workspace := t.TempDir()
	store, err := NewStore(t.TempDir(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	project := store.List()[0]
	tooLarge := strings.Repeat("x", MaxPromptBytes+1)
	if err := store.SetPromptSettings(true, tooLarge); err == nil {
		t.Fatal("expected global prompt size limit")
	}
	if _, err := store.UpdatePrompt(project.ID, tooLarge); err == nil {
		t.Fatal("expected project prompt size limit")
	}
}
