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
		tool.Description = "Capture only the Windows application window selected in MCP DevDesk. Omit window to use the locked target; another window id is rejected. Nothing is saved to disk."
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
		// screenshot: the agent may enumerate visible top-level windows and read
		// them individually, which also covers windows hidden behind others.
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
