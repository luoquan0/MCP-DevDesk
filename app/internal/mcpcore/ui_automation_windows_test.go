//go:build windows

package mcpcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"
)

func TestUIAutomationReadsOwnedNativeWindow(t *testing.T) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true") {
		t.Skip("hosted GitHub Actions has no interactive Windows desktop for UI Automation")
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	createWindowEx := user32.NewProc("CreateWindowExW")
	destroyWindow := user32.NewProc("DestroyWindow")
	className, err := syscall.UTF16PtrFromString("STATIC")
	if err != nil {
		t.Fatal(err)
	}
	titleText := "MCP DevDesk UI Automation Fixture"
	title, err := syscall.UTF16PtrFromString(titleText)
	if err != nil {
		t.Fatal(err)
	}
	hwnd, _, callErr := createWindowEx.Call(
		0,
		uintptr(unsafePointer(className)),
		uintptr(unsafePointer(title)),
		0x00CF0000,
		0, 0, 420, 180,
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		t.Fatalf("CreateWindowExW failed: %v", callErr)
	}
	defer destroyWindow.Call(hwnd)

	nodes, _, err := platformReadUIAutomationTree(screenWindow{ID: "fixture", Handle: hwnd, Title: titleText}, 2, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("UI Automation returned no semantic nodes")
	}
	found := false
	for _, node := range nodes {
		if strings.Contains(node.Name, "MCP DevDesk UI Automation Fixture") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture window name not found in UIA tree: %#v", nodes)
	}
}

func TestUIAutomationPowerShellKeepsPasswordValuesOut(t *testing.T) {
	if !strings.Contains(uiAutomationPowerShell, "if (-not $isPassword)") {
		t.Fatal("UI Automation script no longer protects password value reads")
	}
	encoded := encodePowerShellCommand("A中")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("encoded PowerShell length = %d", len(raw))
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	if got := string(utf16.Decode(units)); got != "A中" {
		t.Fatalf("PowerShell round trip = %q", got)
	}
}

func TestUIAutomationPowerShellHelpersHandleScalarValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := uiAutomationPowerShellHelpers + `
[pscustomobject]@{
  text = (SafeText ([int]42))
  number = (SafeInt ([double]12))
} | ConvertTo-Json -Compress`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShellCommand(script))
	configureCommand(cmd)
	output, err := cmd.Output()
	if ctx.Err() != nil {
		t.Fatal("PowerShell helper regression test timed out")
	}
	if err != nil {
		t.Fatalf("PowerShell helper regression test failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	var payload struct {
		Text   string `json:"text"`
		Number int    `json:"number"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("decode PowerShell helper result: %v: %s", err, string(output))
	}
	if payload.Text != "42" || payload.Number != 12 {
		t.Fatalf("unexpected PowerShell helper result: %#v", payload)
	}
}

func TestUIAutomationPowerShellCollectionPayloadIsJSONSafe(t *testing.T) {
	if !strings.Contains(uiAutomationPowerShell, "$items.ToArray()") {
		t.Fatal("UI Automation script must materialize the Generic.List before JSON conversion")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := uiAutomationPowerShellHelpers + `
$items = New-Object System.Collections.Generic.List[object]
$items.Add([pscustomobject]@{ name='alpha'; frameworkId=(SafeText ([int]42)) }) | Out-Null
$items.Add([pscustomobject]@{ name='beta'; frameworkId='WebView2' }) | Out-Null
$nodeArray = $items.ToArray()
[pscustomobject]@{ nodes=$nodeArray; truncated=$false } | ConvertTo-Json -Depth 8 -Compress`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShellCommand(script))
	configureCommand(cmd)
	output, err := cmd.Output()
	if ctx.Err() != nil {
		t.Fatal("PowerShell collection payload regression test timed out")
	}
	if err != nil {
		t.Fatalf("PowerShell collection payload regression test failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	var payload struct {
		Nodes []struct {
			Name        string `json:"name"`
			FrameworkID string `json:"frameworkId"`
		} `json:"nodes"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("decode PowerShell collection payload: %v: %s", err, string(output))
	}
	if payload.Truncated || len(payload.Nodes) != 2 || payload.Nodes[0].FrameworkID != "42" || payload.Nodes[1].FrameworkID != "WebView2" {
		t.Fatalf("unexpected PowerShell collection payload: %#v", payload)
	}
}

func unsafePointer[T any](value *T) unsafe.Pointer { return unsafe.Pointer(value) }
