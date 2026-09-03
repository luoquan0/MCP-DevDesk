from pathlib import Path


def replace(path, old, new):
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"expected integration marker missing in {path}: {old[:80]!r}")
    p.write_text(text.replace(old, new, 1), encoding="utf-8")

# Go MCP server registration and state.
replace("app/internal/mcpcore/server.go",
    "\tAllowNetwork            bool\n\tAuditPath               string\n",
    "\tAllowNetwork            bool\n\tScreenCaptureEnabled    bool\n\tAuditPath               string\n")
replace("app/internal/mcpcore/server.go",
    "\tallowNetwork            bool\n\ttoolProfile             string\n",
    "\tallowNetwork            bool\n\tscreenCaptureEnabled    bool\n\ttoolProfile             string\n")
replace("app/internal/mcpcore/server.go",
    "\ttools = append(tools, permissionTools()...)\n\tcompatibility := compatibilityTools()\n",
    "\ttools = append(tools, permissionTools()...)\n\tif options.ScreenCaptureEnabled && (options.PermissionMode == \"trusted\" || options.PermissionMode == \"dangerous\") {\n\t\ttools = append(tools, screenTools()...)\n\t}\n\tcompatibility := compatibilityTools()\n")
replace("app/internal/mcpcore/server.go",
    "\t\tallowNetwork:            options.AllowNetwork,\n\t\ttoolProfile:             options.ToolProfile,\n",
    "\t\tallowNetwork:            options.AllowNetwork,\n\t\tscreenCaptureEnabled:    options.ScreenCaptureEnabled,\n\t\ttoolProfile:             options.ToolProfile,\n")

# Route screen tools and report screen state.
replace("app/internal/mcpcore/file_tools.go",
    "\t\t\t\"allowNetwork\":    s.allowNetwork,\n\t\t\t\"fileScope\":       s.fileScope,\n",
    "\t\t\t\"allowNetwork\":         s.allowNetwork,\n\t\t\t\"screenCaptureEnabled\": s.screenCaptureEnabled,\n\t\t\t\"fileScope\":            s.fileScope,\n")
replace("app/internal/mcpcore/file_tools.go",
    "\tcase \"permission_status\", \"request_permissions\":\n\t\treturn s.executePermissionTool(name, arguments)\n",
    "\tcase \"permission_status\", \"request_permissions\":\n\t\treturn s.executePermissionTool(name, arguments)\n\tcase \"screen_list_windows\", \"screen_get_active_window\", \"screen_capture_window\", \"screen_capture_active_window\", \"screen_capture_desktop\":\n\t\treturn s.executeScreenTool(name, arguments)\n")

# mcp-core CLI and manager launch args.
replace("app/cmd/mcp-core/main.go",
    "\tallowNetwork := flag.Bool(\"allow-network\", false, \"allow command sessions to use network-capable tools\")\n",
    "\tallowNetwork := flag.Bool(\"allow-network\", false, \"allow command sessions to use network-capable tools\")\n\tscreenCapture := flag.Bool(\"enable-screen-capture\", false, \"enable opt-in on-demand Windows screen vision tools\")\n")
replace("app/cmd/mcp-core/main.go",
    "\t\tAllowNetwork:            *allowNetwork,\n\t\tAuditPath:               resolvedAuditPath,\n",
    "\t\tAllowNetwork:            *allowNetwork,\n\t\tScreenCaptureEnabled:    *screenCapture,\n\t\tAuditPath:               resolvedAuditPath,\n")
replace("app/internal/process/manager.go",
    "\t\tif strings.TrimSpace(instructionsFile) != \"\" {\n\t\t\targs = append(args, \"--instructions-file\", instructionsFile)\n\t\t}\n",
    "\t\tif strings.TrimSpace(instructionsFile) != \"\" {\n\t\t\targs = append(args, \"--instructions-file\", instructionsFile)\n\t\t}\n\t\tif cfg.ScreenCaptureEnabled {\n\t\t\targs = append(args, \"--enable-screen-capture\")\n\t\t}\n")

# Persist screen opt-in in config/public API.
replace("app/internal/model/types.go",
    "\tAllowNetwork            bool     `json:\"allowNetwork\"`\n\tDomain                  string",
    "\tAllowNetwork            bool     `json:\"allowNetwork\"`\n\tScreenCaptureEnabled    bool     `json:\"screenCaptureEnabled\"`\n\tDomain                  string")
replace("app/internal/model/types.go",
    "\tAllowNetwork            bool     `json:\"allowNetwork\"`\n\tDomain                  string",
    "\tAllowNetwork            bool     `json:\"allowNetwork\"`\n\tScreenCaptureEnabled    bool     `json:\"screenCaptureEnabled\"`\n\tDomain                  string")
replace("app/internal/model/types.go",
    "\t\tAllowNetwork:            c.AllowNetwork,\n\t\tDomain:",
    "\t\tAllowNetwork:            c.AllowNetwork,\n\t\tScreenCaptureEnabled:    c.ScreenCaptureEnabled,\n\t\tDomain:")
replace("app/internal/model/types.go",
    "\tAllowNetwork            *bool     `json:\"allowNetwork\"`\n\tDomain",
    "\tAllowNetwork            *bool     `json:\"allowNetwork\"`\n\tScreenCaptureEnabled    *bool     `json:\"screenCaptureEnabled\"`\n\tDomain")
replace("app/internal/config/store.go",
    "\tif update.AllowNetwork != nil {\n\t\tcfg.AllowNetwork = *update.AllowNetwork\n\t}\n",
    "\tif update.AllowNetwork != nil {\n\t\tcfg.AllowNetwork = *update.AllowNetwork\n\t}\n\tif update.ScreenCaptureEnabled != nil {\n\t\tcfg.ScreenCaptureEnabled = *update.ScreenCaptureEnabled\n\t}\n")
replace("frontend/src/types/api.ts",
    "  allowNetwork: boolean;\n  fileScope: FileScope;",
    "  allowNetwork: boolean;\n  screenCaptureEnabled: boolean;\n  fileScope: FileScope;")

# Stable release workflow must never consume beta/rc tags.
replace(".github/workflows/release.yml",
    "    if: github.event_name == 'workflow_dispatch' || startsWith(github.ref, 'refs/tags/v') || contains(github.event.head_commit.message, '[release]')",
    "    if: github.event_name == 'workflow_dispatch' || (startsWith(github.ref, 'refs/tags/v') && !contains(github.ref_name, '-')) || contains(github.event.head_commit.message, '[release]')")
