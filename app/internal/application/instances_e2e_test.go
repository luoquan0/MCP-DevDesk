package application

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"mcp-devdesk/internal/model"
)

func TestRealMultiInstanceStart(t *testing.T) {
	coreSource := os.Getenv("MCP_DEV_DESK_E2E_CORE")
	if coreSource == "" {
		t.Skip("MCP_DEV_DESK_E2E_CORE is not set")
	}
	if runtime.GOOS != "windows" {
		t.Skip("the supplied build artifact is a Windows executable")
	}
	coreSource, err := filepath.Abs(coreSource)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(coreSource); err != nil {
		t.Fatalf("built Go core is unavailable: %v", err)
	}

	root := t.TempDir()
	if err := copyTestFile(coreSource, filepath.Join(root, "mcp-core.exe")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"coding-tools-mcp.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("not used by this smoke test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := filepath.Join(root, "secondary-workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}

	app, err := New(root, filepath.Join(root, "data", "devdesk"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	port := freeTCPPort(t)
	autoStart := true
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	instance, err := app.CreateInstance(ctx, model.MCPInstanceCreateRequest{
		Name:      "real-core-smoke",
		Workspace: workspace,
		MCPPort:   port,
		CoreMode:  "go",
		AutoStart: &autoStart,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !instance.MCP.Running || instance.MCP.PID <= 0 {
		t.Fatalf("instance did not start: %+v", instance)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/healthz")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("health status = %d: %s", response.StatusCode, body)
	}
	var health struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if !health.OK || health.Version != Version {
		t.Fatalf("unexpected health response: %+v", health)
	}
	if instances := app.Instances(); len(instances) != 2 {
		t.Fatalf("instance count = %d, want 2: %+v", len(instances), instances)
	}

	stopped, err := app.StopInstance(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.MCP.Running {
		t.Fatalf("instance still running after stop: %+v", stopped.MCP)
	}
	if err := app.DeleteInstance(instance.ID); err != nil {
		t.Fatal(err)
	}
	if instances := app.Instances(); len(instances) != 1 || !instances[0].Primary {
		t.Fatalf("instance cleanup failed: %+v", instances)
	}
}

func copyTestFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
