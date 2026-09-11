from pathlib import Path


def replace_exact(path: str, old: str, new: str, count: int = 1) -> None:
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    actual = text.count(old)
    if actual != count:
        raise SystemExit(f"{path}: expected {count} matches, found {actual}")
    p.write_text(text.replace(old, new), encoding="utf-8", newline="\n")


# The UIA PowerShell host may emit CLIXML progress records to stderr on first
# module load. Keep structured JSON on stdout separate from diagnostics.
replace_exact(
    "app/internal/mcpcore/ui_automation_windows.go",
    'import (\n\t"context"',
    'import (\n\t"bytes"\n\t"context"',
)

replace_exact(
    "app/internal/mcpcore/ui_automation_windows.go",
    '''\tconfigureCommand(cmd)\n\toutput, err := cmd.CombinedOutput()\n\tif ctx.Err() != nil {\n\t\treturn nil, false, errors.New("Windows UI Automation read timed out")\n\t}\n\tif err != nil {\n\t\tmessage := strings.TrimSpace(string(output))\n\t\tif len(message) > 1200 {\n\t\t\tmessage = message[:1200] + "..."\n\t\t}\n\t\treturn nil, false, fmt.Errorf("Windows UI Automation read failed: %w: %s", err, message)\n\t}\n\tvar payload uiAutomationPayload\n\tif err := json.Unmarshal(output, &payload); err != nil {\n\t\treturn nil, false, fmt.Errorf("decode Windows UI Automation tree: %w", err)\n\t}\n''',
    '''\tconfigureCommand(cmd)\n\tvar stdout bytes.Buffer\n\tvar stderr bytes.Buffer\n\tcmd.Stdout = &stdout\n\tcmd.Stderr = &stderr\n\terr := cmd.Run()\n\tif ctx.Err() != nil {\n\t\treturn nil, false, errors.New("Windows UI Automation read timed out")\n\t}\n\tif err != nil {\n\t\tmessage := strings.TrimSpace(stderr.String())\n\t\tif message == "" {\n\t\t\tmessage = strings.TrimSpace(stdout.String())\n\t\t}\n\t\tif len(message) > 1200 {\n\t\t\tmessage = message[:1200] + "..."\n\t\t}\n\t\treturn nil, false, fmt.Errorf("Windows UI Automation read failed: %w: %s", err, message)\n\t}\n\tvar payload uiAutomationPayload\n\tif err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {\n\t\treturn nil, false, fmt.Errorf("decode Windows UI Automation tree: %w", err)\n\t}\n''',
)

replace_exact(
    "app/internal/mcpcore/ui_automation_windows_test.go",
    "\toutput, err := cmd.CombinedOutput()\n",
    "\toutput, err := cmd.Output()\n",
)

print("Beta 3 PowerShell stdout/stderr fix applied")
