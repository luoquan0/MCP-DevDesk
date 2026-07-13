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
