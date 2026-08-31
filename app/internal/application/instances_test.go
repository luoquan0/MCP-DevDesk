package application

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"mcp-devdesk/internal/model"
)

func TestAdditionalInstancePersistsAndUsesIsolatedDataDirectory(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"coding-tools-mcp.exe", "mcp-core.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(root, "workspace-two")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "data")
	app, err := New(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	port := freeTCPPort(t)
	created, err := app.CreateInstance(context.Background(), model.MCPInstanceCreateRequest{
		Name:      "second",
		Workspace: workspace,
		MCPPort:   port,
		CoreMode:  "go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Primary || created.ID == "" || created.DataDirectory == dataDir {
		t.Fatalf("unexpected instance: %+v", created)
	}
	if _, err := os.Stat(filepath.Join(created.DataDirectory, "config.json")); err != nil {
		t.Fatalf("isolated config missing: %v", err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := New(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reloaded.Close() })
	instances := reloaded.Instances()
	if len(instances) != 2 {
		t.Fatalf("instance count = %d, want 2: %+v", len(instances), instances)
	}
	found := false
	for _, instance := range instances {
		if instance.ID == created.ID {
			found = true
			if instance.Workspace != workspace || instance.MCPPort != port || instance.Name != "second" {
				t.Fatalf("reloaded instance mismatch: %+v", instance)
			}
		}
	}
	if !found {
		t.Fatalf("instance %s was not reloaded", created.ID)
	}
}

func TestProjectPathChangeKeepsLinkedInstanceAttached(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"coding-tools-mcp.exe", "mcp-core.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldPath := filepath.Join(root, "old-project")
	newPath := filepath.Join(root, "new-project")
	for _, path := range []string{oldPath, newPath} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	app, err := New(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	project, err := app.AddProject("linked", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	port := freeTCPPort(t)
	instance, err := app.CreateInstance(context.Background(), model.MCPInstanceCreateRequest{
		Name:      "linked-instance",
		ProjectID: project.ID,
		MCPPort:   port,
		CoreMode:  "go",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := app.UpdateProjectPath(context.Background(), project.ID, newPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID == project.ID {
		t.Fatal("expected path-based project ID to change")
	}
	linked, err := app.Instance(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if linked.ProjectID != updated.ID || linked.Workspace != filepath.Clean(newPath) {
		t.Fatalf("linked instance was not migrated with project: %+v", linked)
	}
	if err := app.RemoveProject(context.Background(), updated.ID); err != nil {
		t.Fatalf("remove project with linked instance: %v", err)
	}
	if _, err := app.Instance(instance.ID); err == nil {
		t.Fatal("linked MCP instance remained after project removal")
	}
}

func TestRemoveActiveProjectSwitchesToNextProject(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"coding-tools-mcp.exe", "mcp-core.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	nextPath := filepath.Join(root, "next-project")
	if err := os.MkdirAll(nextPath, 0o700); err != nil {
		t.Fatal(err)
	}
	app, err := New(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	port := freeTCPPort(t)
	if _, err := app.UpdateConfig(model.ConfigUpdate{MCPPort: &port}); err != nil {
		t.Fatalf("set isolated MCP port: %v", err)
	}

	var activeProjectID string
	for _, project := range app.Projects() {
		if samePath(project.Path, root) {
			activeProjectID = project.ID
			break
		}
	}
	if activeProjectID == "" {
		t.Fatal("active root project not found")
	}
	nextProject, err := app.AddProject("Next", nextPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RemoveProject(context.Background(), activeProjectID); err != nil {
		t.Fatalf("remove active project: %v", err)
	}
	if !samePath(app.Config().Workspace, nextProject.Path) {
		t.Fatalf("workspace = %q, want %q", app.Config().Workspace, nextProject.Path)
	}
	if _, ok := app.projects.Get(activeProjectID); ok {
		t.Fatal("active project remained after removal")
	}
}

func TestAdditionalInstancesRejectDuplicatePorts(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"coding-tools-mcp.exe", "mcp-core.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspaceA := filepath.Join(root, "a")
	workspaceB := filepath.Join(root, "b")
	if err := os.MkdirAll(workspaceA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspaceB, 0o700); err != nil {
		t.Fatal(err)
	}
	app, err := New(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	port := freeTCPPort(t)
	if _, err := app.CreateInstance(context.Background(), model.MCPInstanceCreateRequest{Name: "a", Workspace: workspaceA, MCPPort: port}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateInstance(context.Background(), model.MCPInstanceCreateRequest{Name: "b", Workspace: workspaceB, MCPPort: port}); err == nil {
		t.Fatal("expected duplicate port error")
	}
}

func TestCloneInstanceSwitchesCoreWithoutCopyingPublicTunnel(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"coding-tools-mcp.exe", "mcp-core.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	app, err := New(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	source, err := app.CreateInstance(context.Background(), model.MCPInstanceCreateRequest{
		Name: "source", Workspace: workspace, MCPPort: freeTCPPort(t), CoreMode: "go", Domain: "mcp.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	clone, err := app.CloneInstance(context.Background(), source.ID, model.MCPInstanceCloneRequest{CoreMode: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if clone.CoreMode != "legacy" || clone.Workspace != source.Workspace || clone.MCPPort == source.MCPPort {
		t.Fatalf("unexpected clone: source=%+v clone=%+v", source, clone)
	}
	if clone.Domain != "" || clone.TunnelID != "" || clone.AutoStart {
		t.Fatalf("clone copied public or running state: %+v", clone)
	}
}

func TestCoreSwitchRequiresStoppedInstanceAndPublicConfirmation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"coding-tools-mcp.exe", "mcp-core.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	app, err := New(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	instance, err := app.CreateInstance(context.Background(), model.MCPInstanceCreateRequest{
		Name: "source", Workspace: workspace, MCPPort: freeTCPPort(t), CoreMode: "go", Domain: "mcp.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	target := "legacy"
	_, runtime, err := app.instanceRecordAndRuntime(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.desiredRunning = true
	if _, err := app.UpdateInstance(context.Background(), instance.ID, model.MCPInstanceUpdateRequest{CoreMode: &target, ConfirmCoreSwitch: true}); err == nil {
		t.Fatal("running instance accepted a core switch")
	}
	runtime.desiredRunning = false
	if _, err := app.UpdateInstance(context.Background(), instance.ID, model.MCPInstanceUpdateRequest{CoreMode: &target}); err == nil {
		t.Fatal("public instance accepted an unconfirmed core switch")
	}
	updated, err := app.UpdateInstance(context.Background(), instance.ID, model.MCPInstanceUpdateRequest{CoreMode: &target, ConfirmCoreSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CoreMode != "legacy" {
		t.Fatalf("core mode = %q", updated.CoreMode)
	}
}

func TestPrimaryCoreSwitchRequiresStoppedServiceAndPublicConfirmation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"coding-tools-mcp.exe", "mcp-core.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app, err := New(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	domain := "mcp.example.com"
	if _, err := app.UpdateConfig(model.ConfigUpdate{Domain: &domain}); err != nil {
		t.Fatal(err)
	}
	target := "go"
	if _, err := app.UpdateConfig(model.ConfigUpdate{CoreMode: &target}); err == nil {
		t.Fatal("public primary instance accepted an unconfirmed core switch")
	}
	app.mu.Lock()
	app.desiredRunning = true
	app.mu.Unlock()
	if _, err := app.UpdateConfig(model.ConfigUpdate{CoreMode: &target, ConfirmCoreSwitch: true}); err == nil {
		t.Fatal("running primary instance accepted a core switch")
	}
	app.mu.Lock()
	app.desiredRunning = false
	app.mu.Unlock()
	updated, err := app.UpdateConfig(model.ConfigUpdate{CoreMode: &target, ConfirmCoreSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CoreMode != "go" {
		t.Fatalf("core mode = %q", updated.CoreMode)
	}
}

func TestAutoStartFailureRollsBackNewInstance(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"coding-tools-mcp.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(root, "rollback-workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "data")
	app, err := New(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	autoStart := true
	_, err = app.CreateInstance(context.Background(), model.MCPInstanceCreateRequest{
		Name:      "rollback",
		Workspace: workspace,
		MCPPort:   freeTCPPort(t),
		CoreMode:  "go",
		AutoStart: &autoStart,
	})
	if err == nil {
		t.Fatal("expected auto-start failure")
	}
	if got := app.Instances(); len(got) != 1 || !got[0].Primary {
		t.Fatalf("failed creation was not rolled back: %+v", got)
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "instances"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("instance data was not cleaned up: %v", entries)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
