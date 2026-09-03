from pathlib import Path

path = Path("tools/apply-screen-vision-modes-fix.py")
text = path.read_text(encoding="utf-8")
old = "    '\\t\"mcp-devdesk/internal/logging\"\\n\\t\"mcp-devdesk/internal/model\"',\n    '\\t\"mcp-devdesk/internal/logging\"\\n\\t\"mcp-devdesk/internal/mcpcore\"\\n\\t\"mcp-devdesk/internal/model\"',"
new = "    '\\tdevlogging \"mcp-devdesk/internal/logging\"\\n\\t\"mcp-devdesk/internal/model\"',\n    '\\tdevlogging \"mcp-devdesk/internal/logging\"\\n\\t\"mcp-devdesk/internal/mcpcore\"\\n\\t\"mcp-devdesk/internal/model\"',"
if old not in text:
    raise RuntimeError("application import patch source not found")
path.write_text(text.replace(old, new, 1), encoding="utf-8")
