package mcpcore

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

const defaultScreenVisionMode = "active"

type screenVisionPolicy struct {
	mode            string
	windowID        string
	windowProcessID uint32
}

var screenVisionPolicies sync.Map

// ConfigureScreenVision narrows the advertised and callable Screen Vision tools
// to the mode explicitly selected in MCP DevDesk. It is called once during Go
// MCP Core startup, before the HTTP server begins serving requests.

func (s *Server) screenVisionDefaultInstructions() string {
	if !s.screenCaptureEnabled || (s.permissionMode != "trusted" && s.permissionMode != "dangerous") {
		return ""
	}
	value, ok := screenVisionPolicies.Load(s)
	if !ok {
		return ""
	}
	policy, ok := value.(screenVisionPolicy)
	if !ok {
		return ""
	}
	switch policy.mode {
	case "desktop":
		return "When the user asks what a named, open, or background application window currently displays, treat it as a GUI-content question and use Screen Vision before process, port, service, or command metadata. Use screen_list_windows to locate the app and screen_capture_window to inspect its pixels. When the user asks what the current foreground window displays, use screen_capture_active_window. If a target is minimized or tray-hidden, call screen_capture_window directly; it automatically attempts a no-focus temporary restore, full-window capture, and restoration of the previous state. Only ask the user to bring the app to the foreground after an actual Screen Vision capture attempt fails or reports the target unavailable. Process/port metadata may supplement the visual result but does not answer what the GUI shows. screen_capture_desktop is for a desktop overview, not a substitute for capturing a named background app."
	case "window":
		return "When the user asks what the selected or locked application window displays, use screen_capture_window first. The locked target may be behind another app or minimized/tray-hidden; capture already handles background access, no-focus temporary restoration when needed, and state restoration. Do not ask the user to foreground the target before trying Screen Vision. Only request foregrounding after the capture tool itself fails. Process/port metadata may supplement the result but cannot replace GUI inspection."
	default:
		return "When the user asks what the current window or currently visible application displays, use screen_capture_active_window first. Do not answer a GUI-content question only from process/port metadata. Only ask the user to change or foreground a window after the Screen Vision capture itself fails."
	}
}

func (s *Server) ConfigureScreenVision(mode, windowID string, windowProcessID uint32) {
	policy := screenVisionPolicy{
		mode:            normalizeScreenVisionMode(mode),
		windowID:        strings.TrimSpace(windowID),
		windowProcessID: windowProcessID,
	}
	screenVisionPolicies.Store(s, policy)

	s.tools = filterTools(s.tools, func(tool Tool) bool {
		if !strings.HasPrefix(tool.Name, "screen_") {
			return true
		}
		return policy.allows(tool.Name)
	})
	if policy.mode != "window" || policy.windowID == "" {
		return
	}
	for index := range s.tools {
		tool := &s.tools[index]
		if tool.Name != "screen_capture_window" {
			continue
		}
		delete(tool.InputSchema, "required")
		tool.Description = "Default GUI inspection tool for the Windows application window selected in MCP DevDesk. Use it first when the user asks what the locked app visually displays; do not substitute process/port metadata or ask the user to foreground it first. The target may be behind another app, minimized, or tray-hidden; capture attempts temporarily restore dormant targets without focus when needed and return them to their previous state. Only ask the user to foreground the app after this capture actually fails. Omit window to use the locked target; another window id is rejected. Nothing is saved to disk."
		if properties, ok := tool.InputSchema["properties"].(map[string]any); ok {
			if windowProperty, ok := properties["window"].(map[string]any); ok {
				windowProperty["description"] = "Optional. Screen Vision is locked to the window selected in MCP DevDesk; another window id is rejected."
			}
		}
	}
}

func normalizeScreenVisionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "window":
		return "window"
	case "desktop":
		return "desktop"
	default:
		return defaultScreenVisionMode
	}
}

func (policy screenVisionPolicy) allows(name string) bool {
	switch policy.mode {
	case "window":
		if name == "screen_list_windows" {
			return true
		}
		return policy.windowID != "" && name == "screen_capture_window"
	case "desktop":
		// Whole-computer mode is intentionally broader than a single desktop
		// screenshot: the agent may enumerate top-level application windows, including
		// minimized targets, and read them individually when Windows allows it.
		return name == "screen_list_windows" ||
			name == "screen_get_active_window" ||
			name == "screen_capture_window" ||
			name == "screen_capture_active_window" ||
			name == "screen_capture_desktop"
	default:
		return name == "screen_get_active_window" || name == "screen_capture_active_window"
	}
}

func (s *Server) enforceScreenVisionToolPolicy(name string) error {
	value, ok := screenVisionPolicies.Load(s)
	if !ok {
		// Keep library/test callers compatible. Production mcp-core always calls
		// ConfigureScreenVision before serving requests.
		return nil
	}
	policy := value.(screenVisionPolicy)
	if policy.allows(name) {
		return nil
	}
	return fmt.Errorf("Screen Vision tool %s is not allowed by the selected %s capture mode", name, policy.mode)
}

func (s *Server) screenVisionWindowArgument(requested string) (string, error) {
	value, ok := screenVisionPolicies.Load(s)
	if !ok {
		return requested, nil
	}
	policy := value.(screenVisionPolicy)
	if policy.mode != "window" {
		return requested, nil
	}
	if policy.windowID == "" {
		return "", errors.New("Screen Vision is in specified-window mode but no window is selected in MCP DevDesk")
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return policy.windowID, nil
	}
	if !strings.EqualFold(requested, policy.windowID) {
		return "", fmt.Errorf("Screen Vision is locked to window %s; requested window %s is not allowed", policy.windowID, requested)
	}
	return policy.windowID, nil
}

func (s *Server) validateScreenVisionWindow(window screenWindow) error {
	value, ok := screenVisionPolicies.Load(s)
	if !ok {
		return nil
	}
	policy := value.(screenVisionPolicy)
	if policy.mode != "window" {
		return nil
	}
	if policy.windowID == "" || !strings.EqualFold(window.ID, policy.windowID) {
		return errors.New("selected Screen Vision window is no longer available")
	}
	if policy.windowProcessID != 0 && window.ProcessID != policy.windowProcessID {
		return errors.New("selected Screen Vision window identity changed; refresh the window list and select it again")
	}
	return nil
}
