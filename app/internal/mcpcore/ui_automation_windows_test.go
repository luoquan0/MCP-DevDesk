//go:build windows

package mcpcore

import (
	"encoding/base64"
	"strings"
	"syscall"
	"testing"
	"unicode/utf16"
	"unsafe"
)

func TestUIAutomationReadsOwnedNativeWindow(t *testing.T) {
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

func unsafePointer[T any](value *T) unsafe.Pointer { return unsafe.Pointer(value) }
