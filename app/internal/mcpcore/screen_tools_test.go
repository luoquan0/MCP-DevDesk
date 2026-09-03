package mcpcore

import (
	"encoding/base64"
	"image"
	"image/png"
	"bytes"
	"strings"
	"testing"
)

func TestScreenToolsRequireExplicitOptIn(t *testing.T) {
	disabled, err := New(Options{Workspace: t.TempDir(), PermissionMode: "trusted"})
	if err != nil {
		t.Fatal(err)
	}
	defer disabled.Close()
	if hasScreenTool(disabled.tools) {
		t.Fatal("screen tools were advertised while Screen Vision was disabled")
	}
	if _, err := disabled.executeTool("screen_list_windows", map[string]any{}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled screen call returned unexpected error: %v", err)
	}

	enabled, err := New(Options{Workspace: t.TempDir(), PermissionMode: "trusted", ScreenCaptureEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer enabled.Close()
	if !hasScreenTool(enabled.tools) {
		t.Fatal("trusted opt-in server did not advertise screen tools")
	}
	status := enabled.permissionStatus()
	capabilities, ok := status["capabilities"].(map[string]bool)
	if !ok || !capabilities["screen"] {
		t.Fatalf("screen capability was not enabled: %#v", status)
	}

	safe, err := New(Options{Workspace: t.TempDir(), PermissionMode: "safe", ScreenCaptureEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer safe.Close()
	if hasScreenTool(safe.tools) {
		t.Fatal("safe mode advertised screen tools")
	}
	if _, err := safe.executeTool("screen_list_windows", map[string]any{}); err == nil || !strings.Contains(err.Error(), "trusted") {
		t.Fatalf("safe mode screen call returned unexpected error: %v", err)
	}
}

func TestResolveScreenWindow(t *testing.T) {
	windows := []screenWindow{
		{ID: "0x10", Handle: 0x10, Title: "MCP DevDesk", ProcessName: "MCP-DevDesk.exe"},
		{ID: "0x20", Handle: 0x20, Title: "Project - Visual Studio Code", ProcessName: "Code.exe"},
		{ID: "0x30", Handle: 0x30, Title: "Docs - Visual Studio Code", ProcessName: "Code.exe"},
	}
	byID, err := resolveScreenWindow(windows, "0x10")
	if err != nil || byID.Handle != 0x10 {
		t.Fatalf("resolve id = %#v, %v", byID, err)
	}
	byExact, err := resolveScreenWindow(windows, "mcp devdesk")
	if err != nil || byExact.Handle != 0x10 {
		t.Fatalf("resolve exact = %#v, %v", byExact, err)
	}
	if _, err := resolveScreenWindow(windows, "visual studio code"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous title error, got %v", err)
	}
	bySubstring, err := resolveScreenWindow(windows, "Project -")
	if err != nil || bySubstring.Handle != 0x20 {
		t.Fatalf("resolve substring = %#v, %v", bySubstring, err)
	}
}

func TestScreenFrameResultProducesPNGContent(t *testing.T) {
	frameImage := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for offset := 0; offset < len(frameImage.Pix); offset += 4 {
		frameImage.Pix[offset] = 10
		frameImage.Pix[offset+1] = 20
		frameImage.Pix[offset+2] = 30
		frameImage.Pix[offset+3] = 255
	}
	result, err := screenFrameResult(screenCaptureFrame{Image: frameImage, Bounds: screenRect{Width: 4, Height: 2}, Method: "test"}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["persisted"] != false || result["continuous"] != false {
		t.Fatalf("unexpected screen metadata: %#v", result)
	}
	encoded, ok := result["_mcpImageData"].(string)
	if !ok || encoded == "" {
		t.Fatalf("missing MCP image data: %#v", result)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 4 || decoded.Bounds().Dy() != 2 {
		t.Fatalf("decoded image size = %v", decoded.Bounds())
	}
}

func hasScreenTool(tools []Tool) bool {
	for _, tool := range tools {
		if strings.HasPrefix(tool.Name, "screen_") {
			return true
		}
	}
	return false
}
