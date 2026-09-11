package mcpcore

import (
	"errors"
	"fmt"
	"strings"
)

type uiAutomationArgs struct {
	Window   string `json:"window,omitempty"`
	MaxDepth int    `json:"maxDepth,omitempty"`
	MaxNodes int    `json:"maxNodes,omitempty"`
}

type uiAutomationNode struct {
	Depth        int        `json:"depth"`
	Name         string     `json:"name,omitempty"`
	AutomationID string     `json:"automationId,omitempty"`
	ControlType  string     `json:"controlType,omitempty"`
	ClassName    string     `json:"className,omitempty"`
	FrameworkID  string     `json:"frameworkId,omitempty"`
	Enabled      bool       `json:"enabled"`
	Offscreen    bool       `json:"offscreen"`
	Password     bool       `json:"password,omitempty"`
	Bounds       screenRect `json:"bounds"`
	Value        string     `json:"value,omitempty"`
}

func uiAutomationTools() []Tool {
	return []Tool{{
		Name:        "ui_automation_tree",
		Title:       "Read Windows UI Semantics",
		Description: "Read the semantic Windows UI Automation tree for the active or Screen Vision-authorized application window. This is read-only, does not click or focus controls, and omits password values.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"window":   map[string]any{"type": "string", "maxLength": 500, "description": "Optional window id/title. Active mode ignores arbitrary targets; specified-window mode is locked to the configured target."},
				"maxDepth": map[string]any{"type": "integer", "minimum": 1, "maximum": 8, "default": 5},
				"maxNodes": map[string]any{"type": "integer", "minimum": 20, "maximum": 1000, "default": 300},
			},
			"additionalProperties": false,
		},
	}}
}

func (s *Server) executeUIAutomationTool(arguments map[string]any) (map[string]any, error) {
	if err := s.requireScreenCapturePermission(); err != nil {
		return nil, err
	}
	var args uiAutomationArgs
	if err := decodeToolArguments(arguments, &args); err != nil {
		return nil, err
	}
	if args.MaxDepth == 0 {
		args.MaxDepth = 5
	}
	if args.MaxNodes == 0 {
		args.MaxNodes = 300
	}
	if args.MaxDepth < 1 || args.MaxDepth > 8 {
		return nil, errors.New("maxDepth must be between 1 and 8")
	}
	if args.MaxNodes < 20 || args.MaxNodes > 1000 {
		return nil, errors.New("maxNodes must be between 20 and 1000")
	}
	window, err := s.resolveUIAutomationWindow(args.Window)
	if err != nil {
		return nil, err
	}
	nodes, truncated, err := platformReadUIAutomationTree(window, args.MaxDepth, args.MaxNodes)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"window":       window,
		"nodes":        nodes,
		"count":        len(nodes),
		"truncated":    truncated,
		"semantic":     true,
		"readOnly":     true,
		"passwordSafe": true,
	}, nil
}

func (s *Server) resolveUIAutomationWindow(requested string) (screenWindow, error) {
	requested = strings.TrimSpace(requested)
	value, hasPolicy := screenVisionPolicies.Load(s)
	if !hasPolicy {
		if requested == "" {
			return platformActiveScreenWindow()
		}
		windows, err := platformListScreenWindowsForVision()
		if err != nil {
			return screenWindow{}, err
		}
		return resolveScreenWindow(windows, requested)
	}
	policy := value.(screenVisionPolicy)
	switch policy.mode {
	case "window":
		locked, err := s.screenVisionWindowArgument(requested)
		if err != nil {
			return screenWindow{}, err
		}
		windows, err := platformListScreenWindowsForVision()
		if err != nil {
			return screenWindow{}, err
		}
		window, err := resolveScreenWindow(windows, locked)
		if err != nil {
			return screenWindow{}, err
		}
		if err := s.validateScreenVisionWindow(window); err != nil {
			return screenWindow{}, err
		}
		return window, nil
	case "desktop":
		if requested == "" {
			return platformActiveScreenWindow()
		}
		windows, err := platformListScreenWindowsForVision()
		if err != nil {
			return screenWindow{}, err
		}
		return resolveScreenWindow(windows, requested)
	default:
		active, err := platformActiveScreenWindow()
		if err != nil {
			return screenWindow{}, err
		}
		if requested == "" || strings.EqualFold(requested, active.ID) || strings.EqualFold(requested, active.Title) {
			return active, nil
		}
		return screenWindow{}, fmt.Errorf("Screen Vision active-window mode only permits the current foreground window %s", active.ID)
	}
}
