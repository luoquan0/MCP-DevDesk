package mcpcore

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWritePatchMoveAndDeleteTools(t *testing.T) {
	workspace := t.TempDir()
	server := mustNewServer(t, Options{Workspace: workspace, PermissionMode: "trusted"})

	writeResult, err := server.executeTool("write_file", map[string]any{
		"path": "nested/example.txt", "content": "alpha beta", "createParents": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if writeResult["bytesWritten"] != 10 {
		t.Fatalf("unexpected write result: %#v", writeResult)
	}

	patchResult, err := server.executeTool("apply_patch", map[string]any{
		"path": "nested/example.txt",
		"operations": []any{
			map[string]any{"oldText": "alpha", "newText": "gamma"},
			map[string]any{"oldText": "beta", "newText": "delta"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if patchResult["replacements"] != 2 {
		t.Fatalf("unexpected patch result: %#v", patchResult)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "nested", "example.txt"))
	if err != nil || string(raw) != "gamma delta" {
		t.Fatalf("patched content = %q, err = %v", string(raw), err)
	}

	if _, err := server.executeTool("move_path", map[string]any{
		"source": "nested/example.txt", "target": "renamed.txt",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "renamed.txt")); err != nil {
		t.Fatal(err)
	}

	if _, err := server.executeTool("delete_path", map[string]any{"path": "renamed.txt"}); err == nil {
		t.Fatal("trusted delete should require confirm=true")
	}
	if _, err := server.executeTool("delete_path", map[string]any{"path": "renamed.txt", "confirm": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "renamed.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file still exists: %v", err)
	}
}

func TestLegacyPatchEnvelope(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "one.txt"), []byte("alpha\nbeta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := mustNewServer(t, Options{Workspace: workspace, PermissionMode: "trusted"})
	patch := `*** Begin Patch
*** Update File: one.txt
@@
 alpha
-beta
+gamma
*** Add File: nested/two.txt
+created
*** End Patch`
	dryRun, err := server.executeTool("apply_patch", map[string]any{"patch": patch, "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if dryRun["applied"] != false || dryRun["count"] != 2 {
		t.Fatalf("unexpected dry-run result: %#v", dryRun)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "one.txt"))
	if string(raw) != "alpha\nbeta\n" {
		t.Fatal("dry run modified the file")
	}
	result, err := server.executeTool("apply_patch", map[string]any{"patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	if result["applied"] != true {
		t.Fatalf("patch was not applied: %#v", result)
	}
	raw, _ = os.ReadFile(filepath.Join(workspace, "one.txt"))
	if string(raw) != "alpha\ngamma\n" {
		t.Fatalf("unexpected patched content: %q", string(raw))
	}
	added, err := os.ReadFile(filepath.Join(workspace, "nested", "two.txt"))
	if err != nil || string(added) != "created\n" {
		t.Fatalf("unexpected added file: %q, %v", string(added), err)
	}
	deletePatch := "*** Begin Patch\n*** Delete File: nested/two.txt\n*** End Patch"
	if _, err := server.executeTool("apply_patch", map[string]any{"patch": deletePatch}); err == nil {
		t.Fatal("trusted patch deletion did not require confirmation")
	}
	if _, err := server.executeTool("apply_patch", map[string]any{"patch": deletePatch, "confirm": true}); err != nil {
		t.Fatal(err)
	}
}

func TestSafeModeRejectsMutationAndCommand(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "safe"})
	if _, err := server.executeTool("write_file", map[string]any{"path": "no.txt", "content": "no"}); err == nil {
		t.Fatal("safe mode allowed file write")
	}
	if _, err := server.executeTool("exec_command", map[string]any{"command": "go", "args": []any{"version"}}); err == nil {
		t.Fatal("safe mode allowed command execution")
	}
	status, err := server.executeTool("permission_status", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if status["permissionMode"] != "safe" {
		t.Fatalf("unexpected permission status: %#v", status)
	}
}

func TestReadOnlyToolProfilesEnforceRestrictions(t *testing.T) {
	readOnly := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "trusted", ToolProfile: "read-only"})
	for _, tool := range readOnly.tools {
		if isMutatingOrCommandTool(tool.Name) {
			t.Fatalf("read-only profile exposed mutating tool %s", tool.Name)
		}
	}
	if _, err := readOnly.executeTool("write_file", map[string]any{"path": "no.txt", "content": "no"}); err == nil {
		t.Fatal("read-only profile allowed a hidden write tool")
	}

	compat := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "trusted", ToolProfile: "compat-readonly-all"})
	foundWrite := false
	for _, tool := range compat.tools {
		if tool.Name == "write_file" {
			foundWrite = true
			break
		}
	}
	if !foundWrite {
		t.Fatal("compat-readonly-all should expose write tool schemas for compatibility")
	}
	if _, err := compat.executeTool("write_file", map[string]any{"path": "no.txt", "content": "no"}); err == nil {
		t.Fatal("compat-readonly-all allowed a write tool")
	}
}

func TestRootsFileScopeAllowsConfiguredAbsolutePath(t *testing.T) {
	workspace := t.TempDir()
	secondRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(secondRoot, "allowed.txt"), []byte("allowed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "denied.txt"), []byte("denied"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := mustNewServer(t, Options{
		Workspace: workspace, FileScope: "roots", AllowedRoots: []string{secondRoot},
	})
	result, err := server.executeTool("read_file", map[string]any{"path": filepath.Join(secondRoot, "allowed.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if result["content"] != "allowed" {
		t.Fatalf("unexpected allowed root result: %#v", result)
	}
	if _, err := server.executeTool("read_file", map[string]any{"path": filepath.Join(outside, "denied.txt")}); err == nil {
		t.Fatal("roots file scope allowed an unconfigured path")
	}
}

func TestCompatibilityToolsAndImages(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "sub", "one.txt"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := mustNewServer(t, Options{Workspace: workspace, PermissionMode: "trusted"})
	if _, err := server.executeTool("set_default_cwd", map[string]any{"path": "sub"}); err != nil {
		t.Fatal(err)
	}
	read, err := server.executeTool("read_file", map[string]any{"path": "one.txt"})
	if err != nil || read["content"] != "one" {
		t.Fatalf("default cwd was not applied: %#v, %v", read, err)
	}
	listed, err := server.executeTool("list_files", map[string]any{"path": ".", "glob": "*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if listed["count"] != 1 {
		t.Fatalf("unexpected recursive list: %#v", listed)
	}

	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	imageResult, err := server.executeTool("write_image", map[string]any{
		"path": "pixel.png", "data": base64.StdEncoding.EncodeToString(png), "mimeType": "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if imageResult["mimeType"] != "image/png" {
		t.Fatalf("unexpected image result: %#v", imageResult)
	}
	viewResult, err := server.executeTool("view_image", map[string]any{"path": "pixel.png"})
	if err != nil {
		t.Fatal(err)
	}
	if viewResult["_mcpImageMimeType"] != "image/png" || viewResult["_mcpImageData"] == "" {
		t.Fatalf("unexpected view image result: %#v", viewResult)
	}
}

func TestLegacyToolNamesRemainAvailable(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	available := make(map[string]bool)
	for _, tool := range server.tools {
		available[tool.Name] = true
	}
	legacyNames := []string{
		"server_info", "check_exec_environment", "get_default_cwd", "set_default_cwd",
		"read_file", "list_dir", "list_files", "search_text", "apply_patch",
		"exec_command", "write_stdin", "kill_session", "read_output",
		"git_status", "git_diff", "git_log", "git_show", "git_blame",
		"request_permissions", "write_image", "save_chatgpt_image", "view_image",
	}
	for _, name := range legacyNames {
		if !available[name] {
			t.Fatalf("legacy tool %s is missing", name)
		}
	}
}

func TestCommandSessionLifecycle(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "trusted", AllowNetwork: true})
	result, err := server.executeTool("exec_command", map[string]any{
		"command": "go", "args": []any{"version"}, "waitMillis": 30000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["running"] == true {
		t.Fatalf("go version should complete: %#v", result)
	}
	if !strings.Contains(strings.ToLower(result["output"].(string)), "go version") {
		t.Fatalf("unexpected command output: %#v", result)
	}
	sessionID, _ := result["sessionId"].(string)
	read, err := server.executeTool("read_output", map[string]any{"sessionId": sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if read["exitCode"] != 0 {
		t.Fatalf("unexpected exit code: %#v", read)
	}
	legacy, err := server.executeTool("exec_command", map[string]any{
		"cmd": "go version", "workdir": ".", "timeout_ms": 60000, "yield_time_ms": 30000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(legacy["output"].(string)), "go version") {
		t.Fatalf("legacy command output: %#v", legacy)
	}
	legacyID := legacy["session_id"].(string)
	if _, err := server.executeTool("read_output", map[string]any{"session_id": legacyID, "max_bytes": 4096}); err != nil {
		t.Fatal(err)
	}
}

func TestCommandEnvironmentFiltersInheritedSecrets(t *testing.T) {
	t.Setenv("MCP_TEST_SECRET_TOKEN", "must-not-leak")
	t.Setenv("MCP_TEST_VISIBLE_VALUE", "visible")
	env := commandEnvironment(nil, true)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "must-not-leak") {
		t.Fatal("sensitive inherited environment variable leaked into command session")
	}
	if !strings.Contains(joined, "MCP_TEST_VISIBLE_VALUE=visible") {
		t.Fatal("non-sensitive environment variable was unexpectedly removed")
	}
}

func TestGitReadTools(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	workspace := t.TempDir()
	runTestGit(t, workspace, "init")
	runTestGit(t, workspace, "config", "user.email", "test@example.com")
	runTestGit(t, workspace, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(workspace, "tracked.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, workspace, "add", "tracked.txt")
	runTestGit(t, workspace, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(workspace, "tracked.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := mustNewServer(t, Options{Workspace: workspace})
	status, err := server.executeTool("git_status", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if status["clean"] != false {
		t.Fatalf("expected dirty Git status: %#v", status)
	}
	diff, err := server.executeTool("git_diff", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff["diff"].(string), "+second") {
		t.Fatalf("unexpected diff: %#v", diff)
	}
	logResult, err := server.executeTool("git_log", map[string]any{"maxCount": 5})
	if err != nil {
		t.Fatal(err)
	}
	if logResult["count"] != 1 {
		t.Fatalf("unexpected log: %#v", logResult)
	}
	blame, err := server.executeTool("git_blame", map[string]any{"path": "tracked.txt", "startLine": 1, "endLine": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(blame["porcelain"].(string), "author Test User") {
		t.Fatalf("unexpected blame output: %#v", blame)
	}
}

func TestAuditLogRedactsContent(t *testing.T) {
	workspace := t.TempDir()
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	server := mustNewServer(t, Options{Workspace: workspace, PermissionMode: "trusted", AuditPath: auditPath})
	started := time.Now()
	toolArguments := map[string]any{"path": "audit.txt", "content": "private-content"}
	_, err := server.executeTool("write_file", toolArguments)
	auditArguments := map[string]any{"path": "audit.txt", "content": "private-content", "token": "secret-token"}
	server.audit.log("write_file", auditArguments, started, err)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("audit log is empty")
	}
	line := scanner.Text()
	if strings.Contains(line, "private-content") || strings.Contains(line, "secret-token") {
		t.Fatalf("audit log leaked sensitive values: %s", line)
	}
	var record auditRecord
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatal(err)
	}
	if !record.Success || record.Tool != "write_file" {
		t.Fatalf("unexpected audit record: %#v", record)
	}
}

func runTestGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	configureCommand(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
