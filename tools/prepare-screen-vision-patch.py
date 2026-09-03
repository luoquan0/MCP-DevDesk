from pathlib import Path

path = Path("tools/apply-screen-vision-modes-fix.py")
text = path.read_text(encoding="utf-8")

# Two model structs intentionally start with the same additive Screen Vision
# field anchor. Let sequential replacements consume the first remaining match
# while still failing loudly when an anchor is missing entirely.
text = text.replace(
    '    if count != 1:\n        raise RuntimeError(f"{path}: expected one match, found {count}: {old[:120]!r}")',
    '    if count < 1:\n        raise RuntimeError(f"{path}: patch anchor not found: {old[:120]!r}")',
    1,
)

old = "    '\\t\"mcp-devdesk/internal/logging\"\\n\\t\"mcp-devdesk/internal/model\"',\n    '\\t\"mcp-devdesk/internal/logging\"\\n\\t\"mcp-devdesk/internal/mcpcore\"\\n\\t\"mcp-devdesk/internal/model\"',"
new = "    '\\tdevlogging \"mcp-devdesk/internal/logging\"\\n\\t\"mcp-devdesk/internal/model\"',\n    '\\tdevlogging \"mcp-devdesk/internal/logging\"\\n\\t\"mcp-devdesk/internal/mcpcore\"\\n\\t\"mcp-devdesk/internal/model\"',"
if old not in text:
    raise RuntimeError("application import patch source not found")
text = text.replace(old, new, 1)

# The generated Go test uses a raw string literal, so JSON quotes must not be
# backslash-escaped inside the raw string.
text = text.replace(
    '[]byte(`{\\"screenCaptureMode\\":\\"window\\",\\"screenCaptureWindowId\\":\\"0x1234\\",\\"screenCaptureWindowProcessId\\":4321}`)',
    '[]byte(`{"screenCaptureMode":"window","screenCaptureWindowId":"0x1234","screenCaptureWindowProcessId":4321}`)',
    1,
)

path.write_text(text, encoding="utf-8")
