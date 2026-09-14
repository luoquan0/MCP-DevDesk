package mcpcore

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV01235CommandSessionHelper(t *testing.T) {
	if os.Getenv("MCP_DEVDESK_COMMAND_HELPER") != "1" {
		return
	}
	fmt.Println("ready")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fmt.Println("echo:" + scanner.Text())
	}
	os.Exit(0)
}

func TestTerminalSessionReadWriteKillAndIdempotency(t *testing.T) {
	workspace := t.TempDir()
	server := mustNewServer(t, Options{Workspace: workspace, PermissionMode: "trusted"})
	defer server.Close()

	started, err := server.executeTool("exec_command", map[string]any{
		"command":    os.Args[0],
		"args":       []any{"-test.run=TestV01235CommandSessionHelper"},
		"cwd":        ".",
		"waitMillis": 100,
		"env":        map[string]any{"MCP_DEVDESK_COMMAND_HELPER": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _ := started["sessionId"].(string)
	if sessionID == "" {
		t.Fatalf("missing session id: %#v", started)
	}

	waitForSessionOutput(t, server, sessionID, "ready")
	if _, err := server.executeTool("write_stdin", map[string]any{"sessionId": sessionID, "chars": "hello\n"}); err != nil {
		t.Fatal(err)
	}
	waitForSessionOutput(t, server, sessionID, "echo:hello")

	killed, err := server.executeTool("kill_session", map[string]any{"sessionId": sessionID, "wait_ms": 5000})
	if err != nil {
		t.Fatal(err)
	}
	if killed["terminated"] != true {
		t.Fatalf("unexpected kill result: %#v", killed)
	}
	output, err := server.executeTool("read_output", map[string]any{"sessionId": sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if output["running"] != false {
		t.Fatalf("session still running after kill: %#v", output)
	}
	again, err := server.executeTool("kill_session", map[string]any{"sessionId": sessionID})
	if err != nil {
		t.Fatalf("second kill should be idempotent: %v", err)
	}
	if again["terminated"] != false || again["completed"] != true {
		t.Fatalf("unexpected second kill result: %#v", again)
	}
}

func waitForSessionOutput(t *testing.T, server *Server, sessionID, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result, err := server.executeTool("read_output", map[string]any{"sessionId": sessionID})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprint(result["output"]), want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for output %q", want)
}

func TestChecksRunReturnsStructuredResult(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.test/checks\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "checks.go"), []byte("package checks\n\nfunc Value() int { return 1 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := mustNewServer(t, Options{Workspace: workspace, PermissionMode: "trusted"})
	defer server.Close()
	result, err := server.executeTool("checks_run", map[string]any{"type": "test", "waitMillis": 30000})
	if err != nil {
		t.Fatal(err)
	}
	if result["structured"] != true || result["detectedRuntime"] != "go" || result["checkType"] != "test" {
		t.Fatalf("unexpected structured check result: %#v", result)
	}
	if result["running"] != false {
		t.Fatalf("Go test should have completed during wait: %#v", result)
	}
}

func TestLexicalCodeNavigationTools(t *testing.T) {
	workspace := t.TempDir()
	source := `package sample

type Widget struct{}

func BuildWidget() *Widget { return &Widget{} }

func UseWidget() *Widget { return BuildWidget() }
`
	if err := os.WriteFile(filepath.Join(workspace, "sample.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	server := mustNewServer(t, Options{Workspace: workspace, PermissionMode: "safe"})

	document, err := server.executeTool("document_symbols", map[string]any{"path": "sample.go"})
	if err != nil {
		t.Fatal(err)
	}
	if document["engine"] != "lexical-fallback" || document["count"].(int) < 3 {
		t.Fatalf("unexpected document symbols: %#v", document)
	}
	workspaceSymbols, err := server.executeTool("workspace_symbols", map[string]any{"query": "Widget"})
	if err != nil {
		t.Fatal(err)
	}
	if workspaceSymbols["count"].(int) < 2 {
		t.Fatalf("unexpected workspace symbols: %#v", workspaceSymbols)
	}
	definitions, err := server.executeTool("find_definition", map[string]any{"symbol": "BuildWidget"})
	if err != nil {
		t.Fatal(err)
	}
	if definitions["count"].(int) != 1 {
		t.Fatalf("unexpected definitions: %#v", definitions)
	}
	references, err := server.executeTool("find_references", map[string]any{"symbol": "BuildWidget"})
	if err != nil {
		t.Fatal(err)
	}
	if references["count"].(int) < 2 {
		t.Fatalf("unexpected references: %#v", references)
	}
}

func TestFormalToolCatalogExcludesAgentTasks(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "trusted"})
	wanted := map[string]bool{
		"validate_project": false, "checks_run": false,
		"exec_command": false, "read_output": false, "write_stdin": false, "kill_session": false,
		"document_symbols": false, "workspace_symbols": false, "find_definition": false, "find_references": false,
	}
	for _, tool := range server.tools {
		if _, ok := wanted[tool.Name]; ok {
			wanted[tool.Name] = true
		}
		if strings.HasPrefix(tool.Name, "task_") {
			t.Fatalf("formal catalog unexpectedly exposes Agent Task tool %q", tool.Name)
		}
	}
	for name, found := range wanted {
		if !found {
			t.Fatalf("formal catalog missing %q", name)
		}
	}
}
