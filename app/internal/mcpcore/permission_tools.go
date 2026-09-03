package mcpcore

import (
	"errors"
	"fmt"
	"strings"
)

type permissionRequestArgs struct {
	Capability string         `json:"capability"`
	Permission string         `json:"permission,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	Scope      string         `json:"scope,omitempty"`
	TTLSeconds int            `json:"ttl_seconds,omitempty"`
	Arguments  map[string]any `json:"arguments,omitempty"`
}

func permissionTools() []Tool {
	empty := map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
	return []Tool{
		{
			Name:        "permission_status",
			Title:       "Permission Status",
			Description: "Return the active permission mode and effective capabilities of the Go MCP core.",
			InputSchema: empty,
		},
		{
			Name:        "request_permissions",
			Title:       "Request Permission Guidance",
			Description: "Check whether a capability is allowed and return the local setting required when it is denied. This tool never silently escalates permissions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"capability":  map[string]any{"type": "string", "enum": []string{"read", "write", "delete", "command", "network", "screen"}},
					"permission":  map[string]any{"type": "string", "enum": []string{"network", "destructive_command", "long_timeout", "sensitive_env", "shell_expansion", "inline_script", "privileged_executable", "write_generated_or_ignored"}, "description": "Legacy permission category."},
					"reason":      map[string]any{"type": "string", "maxLength": 500},
					"tool_name":   map[string]any{"type": "string"},
					"scope":       map[string]any{"type": "string", "enum": []string{"once", "session"}},
					"ttl_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 3600},
					"arguments":   map[string]any{"type": "object"},
				},
				"anyOf":                []any{map[string]any{"required": []string{"capability"}}, map[string]any{"required": []string{"permission"}}},
				"additionalProperties": false,
			},
		},
	}
}

func (s *Server) executePermissionTool(name string, arguments map[string]any) (map[string]any, error) {
	switch name {
	case "permission_status":
		return s.permissionStatus(), nil
	case "request_permissions":
		var args permissionRequestArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		capability := strings.TrimSpace(args.Capability)
		if capability == "" {
			capability = legacyPermissionCapability(args.Permission)
		}
		if capability == "" {
			return nil, errors.New("capability is required")
		}
		allowed, requiredMode := s.capabilityAllowed(capability)
		result := map[string]any{
			"capability":     capability,
			"permission":     strings.TrimSpace(args.Permission),
			"allowed":        allowed,
			"currentMode":    s.permissionMode,
			"reason":         strings.TrimSpace(args.Reason),
			"toolName":       strings.TrimSpace(args.ToolName),
			"scope":          strings.TrimSpace(args.Scope),
			"ttlSeconds":     args.TTLSeconds,
			"selfEscalation": false,
		}
		if !allowed {
			result["requiredMode"] = requiredMode
			if capability == "screen" && !s.screenCaptureEnabled {
				result["message"] = "Open MCP DevDesk Security settings and enable Screen Vision. Screen capture is explicit opt-in and the Go core does not self-enable it."
			} else {
				result["message"] = fmt.Sprintf("Open MCP DevDesk Security settings and switch permission mode to %s. The Go core does not self-escalate.", requiredMode)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unknown permission tool: %s", name)
	}
}

func legacyPermissionCapability(permission string) string {
	switch strings.TrimSpace(permission) {
	case "network":
		return "network"
	case "destructive_command":
		return "delete"
	case "write_generated_or_ignored":
		return "write"
	case "long_timeout", "sensitive_env", "shell_expansion", "inline_script", "privileged_executable":
		return "command"
	default:
		return ""
	}
}

func (s *Server) permissionStatus() map[string]any {
	capabilities := map[string]bool{}
	for _, capability := range []string{"read", "write", "delete", "command", "network", "screen"} {
		capabilities[capability], _ = s.capabilityAllowed(capability)
	}
	return map[string]any{
		"permissionMode":       s.permissionMode,
		"toolProfile":          s.toolProfile,
		"allowNetwork":         s.allowNetwork,
		"screenCaptureEnabled": s.screenCaptureEnabled,
		"fileScope":            s.fileScope,
		"allowedRoots":         append([]string(nil), s.allowedRoots...),
		"capabilities":         capabilities,
		"workspaceBound":       s.fileScope == "workspace",
		"selfEscalation":       false,
	}
}

func (s *Server) capabilityAllowed(capability string) (bool, string) {
	switch capability {
	case "read":
		return true, "safe"
	case "write":
		return s.permissionMode == "trusted" || s.permissionMode == "dangerous", "trusted"
	case "delete", "command":
		return s.permissionMode == "trusted" || s.permissionMode == "dangerous", "trusted"
	case "network":
		return s.allowNetwork && (s.permissionMode == "trusted" || s.permissionMode == "dangerous"), "trusted"
	case "screen":
		return s.screenCaptureEnabled && (s.permissionMode == "trusted" || s.permissionMode == "dangerous"), "trusted"
	default:
		return false, "trusted"
	}
}
